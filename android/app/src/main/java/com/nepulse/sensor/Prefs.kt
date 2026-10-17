package com.nepulse.sensor

import android.content.Context
import android.content.SharedPreferences
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** Everything the app remembers between runs. */
object Prefs {
    private fun p(ctx: Context): SharedPreferences =
        ctx.getSharedPreferences("nepulse", Context.MODE_PRIVATE)

    // Device credentials from enrolment (the secret never leaves this phone
    // except to authenticate with the NE-PULSE server).
    fun creds(ctx: Context): Pair<String, String>? {
        val id = p(ctx).getString("deviceId", null) ?: return null
        val secret = p(ctx).getString("secret", null) ?: return null
        return id to secret
    }

    fun saveCreds(ctx: Context, id: String, secret: String) =
        p(ctx).edit().putString("deviceId", id).putString("secret", secret).apply()

    fun clearCreds(ctx: Context) = p(ctx).edit().remove("deviceId").remove("secret").apply()

    /** Research class code; everyone without one joins the open study. */
    fun cohort(ctx: Context): String = p(ctx).getString("cohort", null) ?: DEFAULT_COHORT
    fun setCohort(ctx: Context, code: String) = p(ctx).edit().putString("cohort", code).apply()
    const val DEFAULT_COHORT = "PUBLIC"

    /** The 1 km grid cell this phone lies in (never the exact position). */
    fun cell(ctx: Context): Pair<Int, Int>? {
        if (!p(ctx).contains("row")) return null
        return p(ctx).getInt("row", 0) to p(ctx).getInt("col", 0)
    }

    fun setCell(ctx: Context, row: Int, col: Int) =
        p(ctx).edit().putInt("row", row).putInt("col", col).apply()

    fun running(ctx: Context) = p(ctx).getBoolean("running", false)
    fun setRunning(ctx: Context, on: Boolean) = p(ctx).edit().putBoolean("running", on).apply()

    fun consented(ctx: Context) = p(ctx).getBoolean("consent", false)
    fun setConsented(ctx: Context) = p(ctx).edit().putBoolean("consent", true).apply()

    fun askedBattery(ctx: Context) = p(ctx).getBoolean("askedBattery", false)
    fun setAskedBattery(ctx: Context) = p(ctx).edit().putBoolean("askedBattery", true).apply()

    fun backend(ctx: Context): String? = p(ctx).getString("backend", null)
    fun setBackend(ctx: Context, url: String) = p(ctx).edit().putString("backend", url).apply()

    // Listening time today (local date), shown on the main screen.
    private fun today(): String = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date())

    fun todaySec(ctx: Context): Double =
        if (p(ctx).getString("day", "") == today()) p(ctx).getFloat("daySec", 0f).toDouble() else 0.0

    fun addTodaySec(ctx: Context, sec: Double) {
        val t = today()
        val base = if (p(ctx).getString("day", "") == t) p(ctx).getFloat("daySec", 0f) else 0f
        p(ctx).edit().putString("day", t).putFloat("daySec", base + sec.toFloat()).apply()
    }

    fun totalSec(ctx: Context): Double = p(ctx).getFloat("totalSec", 0f).toDouble()
    fun addTotalSec(ctx: Context, sec: Double) =
        p(ctx).edit().putFloat("totalSec", p(ctx).getFloat("totalSec", 0f) + sec.toFloat()).apply()
}
