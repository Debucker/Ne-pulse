package com.nepulse.sensor

import android.Manifest
import android.animation.ObjectAnimator
import android.animation.ValueAnimator
import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.provider.Settings
import android.text.InputType
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import java.util.Locale

/**
 * One screen: a status circle, one big button and one line of help.
 * Everything else (class code, privacy) is a small link at the bottom.
 */
class MainActivity : Activity() {

    private val ui = Handler(Looper.getMainLooper())
    private lateinit var ring: View
    private lateinit var status: TextView
    private lateinit var detail: TextView
    private lateinit var button: Button
    private lateinit var today: TextView
    private lateinit var classLink: TextView
    private var pulse: ObjectAnimator? = null
    private val onState: () -> Unit = { ui.post { render() } }

    private fun dp(v: Float) = TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v, resources.displayMetrics).toInt()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = BG
        window.navigationBarColor = BG

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setBackgroundColor(BG)
            setPadding(dp(24f), dp(28f), dp(24f), dp(20f))
        }

        root.addView(TextView(this).apply {
            text = getString(R.string.app_name)
            setTextColor(CYAN)
            textSize = 20f
            typeface = Typeface.create("sans-serif-black", Typeface.NORMAL)
            letterSpacing = 0.08f
        })

        // Status circle.
        val ringBox = FrameLayout(this)
        ring = View(this)
        status = TextView(this).apply {
            setTextColor(INK)
            textSize = 28f
            typeface = Typeface.create("sans-serif-medium", Typeface.BOLD)
            gravity = Gravity.CENTER
        }
        val size = dp(230f)
        ringBox.addView(ring, FrameLayout.LayoutParams(size, size, Gravity.CENTER))
        ringBox.addView(status, FrameLayout.LayoutParams(size, size, Gravity.CENTER))
        root.addView(ringBox, LinearLayout.LayoutParams(size + dp(40f), size + dp(40f)).apply {
            topMargin = dp(36f)
        })

        detail = TextView(this).apply {
            setTextColor(INK2)
            textSize = 19f
            gravity = Gravity.CENTER
            setLineSpacing(0f, 1.15f)
        }
        root.addView(detail, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
            topMargin = dp(18f)
        })

        button = Button(this).apply {
            textSize = 22f
            isAllCaps = false
            typeface = Typeface.create("sans-serif-medium", Typeface.BOLD)
            setOnClickListener { onButton() }
        }
        root.addView(button, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, dp(68f)).apply {
            topMargin = dp(28f)
        })

        today = TextView(this).apply {
            setTextColor(INK2)
            textSize = 17f
            gravity = Gravity.CENTER
        }
        root.addView(today, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
            topMargin = dp(16f)
        })

        root.addView(View(this), LinearLayout.LayoutParams(1, 0, 1f))

        val links = LinearLayout(this).apply { gravity = Gravity.CENTER }
        classLink = link(R.string.link_class) { askClassCode() }
        links.addView(classLink)
        links.addView(link(R.string.link_help) { open("${Api.SITE}/app") })
        links.addView(link(R.string.link_privacy) { open("${Api.SITE}/privacy") })
        root.addView(links)

        setContentView(root)

        // Automated test (debug builds only): start without taps.
        if (BuildConfig.DEBUG && intent.getBooleanExtra("test_autostart", false)) {
            Prefs.setConsented(this)
            Prefs.setCell(this, intent.getIntExtra("row", 0), intent.getIntExtra("col", 0))
            intent.getStringExtra("cohort")?.let { Prefs.setCohort(this, it) }
            SensorService.start(this)
        }
    }

    private fun link(res: Int, onClick: () -> Unit) = TextView(this).apply {
        text = getString(res)
        setTextColor(INK3)
        textSize = 15f
        setPadding(dp(12f), dp(12f), dp(12f), dp(12f))
        paint.isUnderlineText = true
        setOnClickListener { onClick() }
    }

    override fun onResume() {
        super.onResume()
        SensorService.listeners.add(onState)
        render()
        ui.postDelayed(tick, 30_000)
    }

    override fun onPause() {
        SensorService.listeners.remove(onState)
        ui.removeCallbacks(tick)
        super.onPause()
    }

    private val tick: Runnable = object : Runnable {
        override fun run() {
            render()
            ui.postDelayed(this, 30_000)
        }
    }

    private fun render() {
        val running = Prefs.running(this)
        val st = if (running && SensorService.state == "off") "connecting" else SensorService.state
        val color = StateText.color(st)
        status.text = getString(StateText.short(st))
        detail.text = getString(StateText.detail(st))
        (ring.background as? GradientDrawable ?: GradientDrawable().also { ring.background = it }).apply {
            shape = GradientDrawable.OVAL
            setColor((color and 0x00FFFFFF) or 0x22000000)
            setStroke(dp(6f), color)
        }
        val listening = st == "quiet" || st == "triggered"
        if (listening && pulse == null) {
            pulse = ObjectAnimator.ofFloat(ring, View.ALPHA, 1f, 0.55f).apply {
                duration = 1400
                repeatMode = ValueAnimator.REVERSE
                repeatCount = ValueAnimator.INFINITE
                start()
            }
        } else if (!listening) {
            pulse?.cancel()
            pulse = null
            ring.alpha = 1f
        }

        button.text = getString(if (running) R.string.btn_stop else R.string.btn_start)
        button.setTextColor(if (running) INK else BG)
        button.background = GradientDrawable().apply {
            cornerRadius = dp(34f).toFloat()
            if (running) {
                setColor(0x00000000)
                setStroke(dp(2f), INK3)
            } else setColor(CYAN)
        }

        val h = Prefs.todaySec(this) / 3600
        val total = Prefs.totalSec(this) / 3600
        today.text = getString(R.string.today, String.format(Locale.getDefault(), "%.1f", h), String.format(Locale.getDefault(), "%.1f", total))
        today.visibility = if (total > 0.05 || running) View.VISIBLE else View.GONE

        val c = Prefs.cohort(this)
        classLink.text = if (c != Prefs.DEFAULT_COHORT) getString(R.string.link_class_set, c) else getString(R.string.link_class)
    }

    // ------------------------------------------------------------ start flow

    private fun onButton() {
        if (Prefs.running(this)) {
            AlertDialog.Builder(this)
                .setMessage(R.string.stop_confirm)
                .setPositiveButton(R.string.btn_stop) { _, _ -> SensorService.stop(this); render() }
                .setNegativeButton(R.string.keep, null)
                .show()
        } else startFlow()
    }

    private fun startFlow() {
        if (!Prefs.consented(this)) {
            AlertDialog.Builder(this)
                .setTitle(R.string.consent_title)
                .setMessage(R.string.consent_text)
                .setPositiveButton(R.string.consent_ok) { _, _ -> Prefs.setConsented(this); startFlow() }
                .setNeutralButton(R.string.link_privacy) { _, _ -> open("${Api.SITE}/privacy") }
                .setNegativeButton(R.string.cancel, null)
                .show()
            return
        }
        if (Prefs.cell(this) == null) {
            ensureLocation()
            return
        }
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED &&
            !asked("notif")
        ) {
            markAsked("notif")
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQ_NOTIF)
            return
        }
        SensorService.start(this)
        render()
        askBattery()
    }

    private fun asked(k: String) = getSharedPreferences("ask", Context.MODE_PRIVATE).getBoolean(k, false)
    private fun markAsked(k: String) = getSharedPreferences("ask", Context.MODE_PRIVATE).edit().putBoolean(k, true).apply()

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        val ok = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
        when (requestCode) {
            REQ_LOC -> if (ok) locate() else AlertDialog.Builder(this)
                .setMessage(R.string.loc_needed)
                .setPositiveButton(R.string.try_again) { _, _ -> ensureLocation() }
                .setNegativeButton(R.string.cancel, null)
                .show()
            REQ_NOTIF -> startFlow()
        }
    }

    private fun ensureLocation() {
        if (checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(arrayOf(Manifest.permission.ACCESS_COARSE_LOCATION), REQ_LOC)
        } else locate()
    }

    /** Finds the phone's area once, rounds it to a 1 km cell and forgets the rest. */
    @Suppress("MissingPermission", "DEPRECATION")
    private fun locate() {
        val lm = getSystemService(Context.LOCATION_SERVICE) as LocationManager
        val providers = listOf(LocationManager.NETWORK_PROVIDER, LocationManager.GPS_PROVIDER).filter {
            try { lm.isProviderEnabled(it) } catch (e: Exception) { false }
        }
        if (providers.isEmpty()) {
            AlertDialog.Builder(this)
                .setMessage(R.string.loc_off)
                .setPositiveButton(R.string.open_settings) { _, _ -> startActivity(Intent(Settings.ACTION_LOCATION_SOURCE_SETTINGS)) }
                .setNegativeButton(R.string.cancel, null)
                .show()
            return
        }
        status.text = getString(R.string.st_locating)
        var done = false
        val listener = object : LocationListener {
            override fun onLocationChanged(l: Location) = finishLocation(l)
            @Deprecated("") override fun onStatusChanged(p: String?, s: Int, e: Bundle?) {}
            override fun onProviderEnabled(p: String) {}
            override fun onProviderDisabled(p: String) {}
            fun finishLocation(l: Location?) {
                if (done) return
                done = true
                lm.removeUpdates(this)
                if (l == null) {
                    AlertDialog.Builder(this@MainActivity)
                        .setMessage(R.string.loc_fail)
                        .setPositiveButton(R.string.try_again) { _, _ -> locate() }
                        .setNegativeButton(R.string.cancel, null)
                        .show()
                    render()
                    return
                }
                val cell = Grid.cellOf(l.latitude, l.longitude)
                Prefs.setCell(this@MainActivity, cell.first, cell.second)
                startFlow()
            }
        }
        val recent = providers.mapNotNull { try { lm.getLastKnownLocation(it) } catch (e: Exception) { null } }
            .maxByOrNull { it.time }
        if (recent != null && System.currentTimeMillis() - recent.time < 6 * 3600_000L) {
            listener.finishLocation(recent)
            return
        }
        for (p in providers) {
            try { lm.requestLocationUpdates(p, 0L, 0f, listener, Looper.getMainLooper()) } catch (e: Exception) {}
        }
        ui.postDelayed({ listener.finishLocation(recent) }, 25_000)
    }

    private fun askBattery() {
        if (Build.VERSION.SDK_INT < 23 || Prefs.askedBattery(this)) return
        val pm = getSystemService(Context.POWER_SERVICE) as PowerManager
        if (pm.isIgnoringBatteryOptimizations(packageName)) return
        Prefs.setAskedBattery(this)
        AlertDialog.Builder(this)
            .setTitle(R.string.battery_title)
            .setMessage(R.string.battery_text)
            .setPositiveButton(R.string.allow) { _, _ ->
                try {
                    startActivity(Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName")))
                } catch (e: Exception) {
                    startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
                }
            }
            .setNegativeButton(R.string.later, null)
            .show()
    }

    // ------------------------------------------------------------ class code

    private fun askClassCode() {
        val input = EditText(this).apply {
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS
            hint = "8A11UZ"
            val c = Prefs.cohort(this@MainActivity)
            if (c != Prefs.DEFAULT_COHORT) setText(c)
        }
        val box = FrameLayout(this).apply { setPadding(dp(24f), dp(8f), dp(24f), 0); addView(input) }
        AlertDialog.Builder(this)
            .setTitle(R.string.class_title)
            .setMessage(R.string.class_text)
            .setView(box)
            .setPositiveButton(R.string.save) { _, _ -> saveClass(input.text.toString().trim().uppercase(Locale.ROOT)) }
            .setNegativeButton(R.string.cancel, null)
            .show()
    }

    private fun saveClass(code: String) {
        val target = code.ifEmpty { Prefs.DEFAULT_COHORT }
        Thread {
            val msg = try {
                val base = Api.backend(this, false)
                if (!Api.cohortExists(base, target)) throw ClassCodeError()
                Prefs.creds(this)?.let { Api.setCohort(base, it, target) }
                Prefs.setCohort(this, target)
                R.string.class_saved
            } catch (e: ClassCodeError) {
                R.string.class_bad
            } catch (e: Exception) {
                R.string.class_offline
            }
            ui.post {
                AlertDialog.Builder(this).setMessage(msg).setPositiveButton(android.R.string.ok, null).show()
                render()
            }
        }.start()
    }

    private fun open(url: String) {
        try { startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url))) } catch (e: Exception) {}
    }

    companion object {
        private const val REQ_LOC = 1
        private const val REQ_NOTIF = 2
        private const val BG = 0xFF05080B.toInt()
        private const val CYAN = 0xFF38E1FF.toInt()
        private const val INK = 0xFFEAF1F4.toInt()
        private const val INK2 = 0xFFB5C3CA.toInt()
        private const val INK3 = 0xFF7D8E97.toInt()
    }
}
