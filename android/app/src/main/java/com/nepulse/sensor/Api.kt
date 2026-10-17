package com.nepulse.sensor

import android.content.Context
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

class ClassCodeError : Exception("unknown class code")

/** HTTP side of the NE-PULSE device API (docs/API.md). */
object Api {
    const val SITE = "https://www.ne-pulse.com"
    val CLIENT: String get() = "android/" + BuildConfig.VERSION_NAME

    val http: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(15, TimeUnit.SECONDS)
        .pingInterval(20, TimeUnit.SECONDS)
        .build()

    private val JSON = "application/json".toMediaType()

    private fun get(url: String): JSONObject {
        http.newCall(Request.Builder().url(url).header("Cache-Control", "no-cache").build()).execute().use { r ->
            val body = r.body?.string() ?: ""
            if (!r.isSuccessful) throw Exception("HTTP ${r.code}")
            return JSONObject(body)
        }
    }

    /**
     * The server's current addresses. The server runs behind tunnels whose
     * names change; the site always publishes the live list.
     */
    fun backend(ctx: Context, refresh: Boolean): String {
        val saved = Prefs.backend(ctx)
        if (!refresh && saved != null) return saved
        val arr = get("$SITE/backend.json?t=${System.currentTimeMillis()}").getJSONArray("api")
        val list = (0 until arr.length()).map { arr.getString(it).trimEnd('/') }.filter { it.startsWith("http") }
        if (list.isEmpty()) throw Exception("no server address published")
        val pick = if (saved != null && saved in list && !refresh) saved else list.random()
        Prefs.setBackend(ctx, pick)
        return pick
    }

    /** Registers this phone (proof of work, then consent v1). */
    fun enroll(base: String, cohort: String): Pair<String, String> {
        val ch = get("$base/api/v1/challenge")
        val challenge = ch.getString("challenge")
        val nonce = solve(challenge, ch.getInt("difficulty"))
        val body = JSONObject()
            .put("challenge", challenge).put("nonce", nonce).put("platform", "android")
            .put("consent", "v1").put("cohort", cohort).put("ref", "")
        http.newCall(Request.Builder().url("$base/api/v1/devices").post(body.toString().toRequestBody(JSON)).build())
            .execute().use { r ->
                val txt = r.body?.string() ?: ""
                if (r.code == 400 && txt.contains("class code")) throw ClassCodeError()
                if (r.code != 201) throw Exception("enrol HTTP ${r.code}")
                val j = JSONObject(txt)
                return j.getString("deviceId") to j.getString("secret")
            }
    }

    /** Moves an already registered phone into a class (or "" for none). */
    fun setCohort(base: String, creds: Pair<String, String>, cohort: String) {
        val body = JSONObject().put("cohort", cohort).toString().toRequestBody(JSON)
        http.newCall(
            Request.Builder().url("$base/api/v1/devices/me/cohort").post(body)
                .header("X-Device-Id", creds.first).header("Authorization", "Bearer ${creds.second}").build()
        ).execute().use { r ->
            if (r.code == 400) throw ClassCodeError()
            if (!r.isSuccessful) throw Exception("cohort HTTP ${r.code}")
        }
    }

    /** True if the class code exists. */
    fun cohortExists(base: String, code: String): Boolean =
        http.newCall(Request.Builder().url("$base/api/v1/cohorts/${code.trim()}").build()).execute().use { it.isSuccessful }

    fun solve(challenge: String, difficulty: Int): String {
        val md = MessageDigest.getInstance("SHA-256")
        var i = 0L
        while (true) {
            val n = i.toString()
            val h = md.digest("$challenge:$n".toByteArray())
            if (leadingZeroBits(h) >= difficulty) return n
            i++
        }
    }

    private fun leadingZeroBits(h: ByteArray): Int {
        var n = 0
        for (b in h) {
            val v = b.toInt() and 0xff
            if (v == 0) {
                n += 8
                continue
            }
            return n + Integer.numberOfLeadingZeros(v) - 24
        }
        return n
    }
}
