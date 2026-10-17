package com.nepulse.sensor

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.IBinder
import android.os.PowerManager
import android.os.SystemClock
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONArray
import org.json.JSONObject
import java.util.ArrayDeque
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executors
import kotlin.math.abs
import kotlin.math.min
import kotlin.math.roundToInt
import kotlin.math.roundToLong

/**
 * Keeps listening with the screen off: a foreground service (visible as a
 * permanent notification) holding a partial wake lock, reading the
 * accelerometer at ~60 Hz and streaming 1-second batches to the server.
 * Reconnects by itself; restarted after a reboot by [BootReceiver].
 */
class SensorService : Service(), SensorEventListener {

    companion object {
        /** What the main screen shows: off, connecting, offline, warmup, quiet, triggered, disturbed, nolocation, nosensor. */
        @Volatile var state = "off"
        @Volatile var lastError: String? = null
        val listeners = CopyOnWriteArrayList<() -> Unit>()

        fun start(ctx: Context) {
            Prefs.setRunning(ctx, true)
            val i = Intent(ctx, SensorService::class.java)
            if (Build.VERSION.SDK_INT >= 26) ctx.startForegroundService(i) else ctx.startService(i)
        }

        fun stop(ctx: Context) {
            Prefs.setRunning(ctx, false)
            ctx.stopService(Intent(ctx, SensorService::class.java))
        }

        private const val NOTIF_ID = 1
        private const val CHANNEL = "listening"
        private const val BATCH_MS = 1000L
        private const val MAX_QUEUE = 60 // seconds of data kept while reconnecting
        private const val SYNC_EVERY_MS = 30_000L
    }

    private lateinit var thread: HandlerThread
    private lateinit var h: Handler
    private val net = Executors.newSingleThreadExecutor()
    private var wake: PowerManager.WakeLock? = null
    private var sm: SensorManager? = null

    // Clock: one fixed base so the "device clock" never jumps; the server
    // measures its offset from that through sync probes.
    private val baseWallMs = System.currentTimeMillis().toDouble()
    private val baseElapsedNs = SystemClock.elapsedRealtimeNanos()
    private var tsShiftNs: Long? = null // sensor timestamp -> elapsedRealtimeNanos
    private fun deviceNowMs() = baseWallMs + (SystemClock.elapsedRealtimeNanos() - baseElapsedNs) / 1e6

    // Samples of the batch being filled (sensor thread only).
    private val bt = ArrayList<Double>(128)
    private val bx = ArrayList<Int>(128)
    private val by = ArrayList<Int>(128)
    private val bz = ArrayList<Int>(128)

    private class Batch(val t0: Double, val dt: LongArray, val x: IntArray, val y: IntArray, val z: IntArray)

    private val queue = ArrayDeque<Batch>()

    // Connection (handler thread only).
    private var ws: WebSocket? = null
    private var connecting = false
    private var ready = false // welcome received
    private var synced = false
    private var seq = 0L
    private var failures = 0
    private var offsetMs = 0.0
    private var rttMs = 0.0
    private val syncs = ArrayDeque<Pair<Double, Double>>() // (rtt, offset)
    private var lastStatusAt = 0L
    private var lastEligible = false
    private var stopped = false

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        thread = HandlerThread("nepulse").also { it.start() }
        h = Handler(thread.looper)
        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
        wake = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "nepulse:listen").also { it.acquire() }
        startInForeground()
        sm = getSystemService(Context.SENSOR_SERVICE) as SensorManager
        val acc = sm?.getDefaultSensor(Sensor.TYPE_ACCELEROMETER)
        if (acc == null) {
            setState("nosensor")
        } else {
            sm?.registerListener(this, acc, 16_667, 0, h)
        }
        h.postDelayed({ flush() }, BATCH_MS)
        h.post { connect() }
        h.postDelayed({ syncLoop() }, SYNC_EVERY_MS)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Prefs.setRunning(this, true)
        return START_STICKY
    }

    override fun onDestroy() {
        stopped = true
        sm?.unregisterListener(this)
        h.removeCallbacksAndMessages(null)
        ws?.let {
            it.send("""{"type":"bye"}""")
            it.close(1000, "stopped")
        }
        ws = null
        wake?.let { if (it.isHeld) it.release() }
        thread.quitSafely()
        net.shutdown()
        setState("off")
        super.onDestroy()
    }

    // ------------------------------------------------------------ sensor

    override fun onSensorChanged(e: SensorEvent) {
        val shift = tsShiftNs ?: run {
            // Most phones stamp sensor events with elapsedRealtimeNanos; some
            // old ones use System.nanoTime. Pick whichever is closer.
            val er = SystemClock.elapsedRealtimeNanos()
            val nt = System.nanoTime()
            val s = if (abs(e.timestamp - er) <= abs(e.timestamp - nt)) 0L else er - nt
            tsShiftNs = s
            s
        }
        val tMs = baseWallMs + (e.timestamp + shift - baseElapsedNs) / 1e6
        if (bt.isNotEmpty() && tMs < bt[bt.size - 1]) return // out of order
        bt.add(tMs)
        bx.add((e.values[0] * 1000).roundToInt())
        by.add((e.values[1] * 1000).roundToInt())
        bz.add((e.values[2] * 1000).roundToInt())
        if (bt.size >= 590) cut()
    }

    override fun onAccuracyChanged(sensor: Sensor?, accuracy: Int) {}

    private fun cut() {
        if (bt.isEmpty()) return
        val t0 = bt[0]
        val n = bt.size
        val dt = LongArray(n) { ((bt[it] - t0) * 10).roundToLong() }
        for (i in 1 until n) if (dt[i] < dt[i - 1]) dt[i] = dt[i - 1]
        queue.addLast(Batch(t0, dt, bx.toIntArray(), by.toIntArray(), bz.toIntArray()))
        while (queue.size > MAX_QUEUE) queue.removeFirst()
        bt.clear(); bx.clear(); by.clear(); bz.clear()
    }

    private fun flush() {
        cut()
        drain()
        h.postDelayed({ flush() }, BATCH_MS)
    }

    private fun drain() {
        val w = ws ?: return
        if (!ready || !synced) return
        val now = deviceNowMs() + offsetMs
        while (queue.isNotEmpty()) {
            val b = queue.peekFirst()!!
            // The server refuses data older than 90 s.
            if (b.t0 + offsetMs < now - 80_000) {
                queue.removeFirst()
                continue
            }
            val j = JSONObject()
                .put("type", "batch").put("seq", seq + 1).put("t0", b.t0)
                .put("dt", JSONArray().apply { b.dt.forEach { put(it) } })
                .put("x", JSONArray().apply { b.x.forEach { put(it) } })
                .put("y", JSONArray().apply { b.y.forEach { put(it) } })
                .put("z", JSONArray().apply { b.z.forEach { put(it) } })
                .put("clock", JSONObject().put("offset", offsetMs).put("rtt", rttMs))
            if (!w.send(j.toString())) break
            seq++
            queue.removeFirst()
        }
    }

    // ------------------------------------------------------------ network

    private fun connect() {
        if (stopped || ws != null || connecting) return
        connecting = true
        if (state != "warmup" && state != "quiet" && state != "disturbed") setState("connecting")
        val refresh = failures >= 2
        net.execute {
            try {
                val base = Api.backend(this, refresh)
                var creds = Prefs.creds(this)
                if (creds == null) {
                    creds = Api.enroll(base, Prefs.cohort(this))
                    Prefs.saveCreds(this, creds.first, creds.second)
                }
                val cell = Prefs.cell(this)
                h.post {
                    if (cell == null) {
                        connecting = false
                        setState("nolocation")
                    } else open(base, creds, cell)
                }
            } catch (e: ClassCodeError) {
                // The class code no longer exists: join the open study instead.
                Prefs.setCohort(this, Prefs.DEFAULT_COHORT)
                h.post { connecting = false; retry() }
            } catch (e: Exception) {
                lastError = e.message
                h.post { connecting = false; retry() }
            }
        }
    }

    private fun retry() {
        if (stopped) return
        failures++
        if (state != "nolocation") setState("offline")
        val delay = min(60_000L, 2_000L shl min(failures, 5))
        h.postDelayed({ connect() }, delay)
    }

    private fun open(base: String, creds: Pair<String, String>, cell: Pair<Int, Int>) {
        val url = base.replaceFirst("http", "ws") + "/ws/device"
        val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                val hello = JSONObject()
                    .put("type", "hello").put("deviceId", creds.first).put("secret", creds.second)
                    .put("platform", "android").put("client", Api.CLIENT)
                    .put("location", JSONObject().put("grid", Grid.CELL_KM).put("row", cell.first).put("col", cell.second))
                webSocket.send(hello.toString())
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                h.post { if (webSocket === ws) handle(text) }
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                lastError = t.message
                h.post { if (webSocket === ws) dropped() }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                h.post { if (webSocket === ws) dropped() }
            }
        }
        ws = Api.http.newWebSocket(Request.Builder().url(url).build(), listener)
    }

    private fun dropped() {
        ws = null
        ready = false
        synced = false
        connecting = false
        retry()
    }

    private fun handle(text: String) {
        val j = try { JSONObject(text) } catch (e: Exception) { return }
        when (j.optString("type")) {
            "welcome" -> {
                ready = true
                connecting = false
                failures = 0
                seq = 0
                syncs.clear()
                repeat(3) { i -> h.postDelayed({ sendSync() }, i * 300L) }
            }
            "sync" -> {
                val now = deviceNowMs()
                val c0 = j.optDouble("c0")
                val s = j.optDouble("s")
                if (!c0.isNaN() && !s.isNaN()) {
                    syncs.addLast((now - c0) to (s - (c0 + now) / 2))
                    while (syncs.size > 8) syncs.removeFirst()
                    val best = syncs.minByOrNull { it.first }!!
                    rttMs = best.first
                    offsetMs = best.second
                    synced = true
                    drain()
                }
            }
            "status" -> {
                val st = j.optString("state", "warmup")
                val eligible = j.optBoolean("eligible", false)
                val now = SystemClock.elapsedRealtime()
                if (lastEligible && lastStatusAt > 0) {
                    val sec = min(30.0, (now - lastStatusAt) / 1000.0)
                    Prefs.addTodaySec(this, sec)
                    Prefs.addTotalSec(this, sec)
                }
                lastEligible = eligible
                lastStatusAt = now
                setState(st)
            }
            "error" -> {
                when (j.optString("code")) {
                    "auth" -> Prefs.clearCreds(this) // forgotten by the server: register again
                    "location" -> setState("nolocation")
                }
                lastError = j.optString("message")
            }
        }
    }

    private fun sendSync() {
        ws?.send(JSONObject().put("type", "sync").put("c0", deviceNowMs()).toString())
    }

    private fun syncLoop() {
        if (ready) sendSync()
        h.postDelayed({ syncLoop() }, SYNC_EVERY_MS)
    }

    // ------------------------------------------------------------ UI

    private fun setState(s: String) {
        android.util.Log.i("NEPULSE", "state=$s" + (lastError?.let { " error=$it" } ?: ""))
        val changed = s != state
        state = s
        if (changed) {
            updateNotification()
            listeners.forEach { it() }
        }
    }

    private fun startInForeground() {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= 26) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL, getString(R.string.notif_channel), NotificationManager.IMPORTANCE_LOW)
            )
        }
        val n = buildNotification()
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    private fun buildNotification(): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val text = getString(StateText.short(state))
        @Suppress("DEPRECATION")
        val b = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, CHANNEL) else Notification.Builder(this)
        return b.setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(getString(R.string.app_name))
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(open)
            .build()
    }

    private fun updateNotification() {
        if (stopped) return
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        nm.notify(NOTIF_ID, buildNotification())
    }
}
