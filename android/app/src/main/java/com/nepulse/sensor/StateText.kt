package com.nepulse.sensor

/** Plain words for each state, and its colour on the main screen. */
object StateText {
    fun short(state: String): Int = when (state) {
        "quiet", "triggered" -> R.string.st_listening
        "warmup" -> R.string.st_warmup
        "disturbed" -> R.string.st_moving
        "connecting" -> R.string.st_connecting
        "offline" -> R.string.st_offline
        "nolocation" -> R.string.st_nolocation
        "nosensor" -> R.string.st_nosensor
        else -> R.string.st_off
    }

    fun detail(state: String): Int = when (state) {
        "quiet", "triggered" -> R.string.dt_listening
        "warmup" -> R.string.dt_warmup
        "disturbed" -> R.string.dt_moving
        "connecting" -> R.string.dt_connecting
        "offline" -> R.string.dt_offline
        "nolocation" -> R.string.dt_nolocation
        "nosensor" -> R.string.dt_nosensor
        else -> R.string.dt_off
    }

    fun color(state: String): Int = when (state) {
        "quiet", "triggered" -> 0xFF35E0A1.toInt()
        "warmup", "connecting" -> 0xFF38E1FF.toInt()
        "disturbed", "offline", "nolocation" -> 0xFFFFB44D.toInt()
        "nosensor" -> 0xFFFF5D5D.toInt()
        else -> 0xFF5A6B75.toInt()
    }
}
