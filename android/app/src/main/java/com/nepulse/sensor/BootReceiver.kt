package com.nepulse.sensor

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** After a reboot or an app update, carry on listening if the user had started it. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        if (Prefs.running(ctx) && Prefs.cell(ctx) != null) {
            try {
                SensorService.start(ctx)
            } catch (e: Exception) {
                // Some systems refuse background starts; the user can tap Start.
            }
        }
    }
}
