package com.nepulse.sensor

import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.floor
import kotlin.math.roundToLong

/**
 * The server's equal-area grid (internal/geo.Grid in the Go code), so the
 * phone can round its position to a 1 km cell before anything is sent.
 */
object Grid {
    private const val EARTH_RADIUS_KM = 6371.0088
    private const val KM_PER_DEG_LAT = 2 * PI * EARTH_RADIUS_KM / 360
    const val CELL_KM = 1.0

    private fun dLat() = CELL_KM / KM_PER_DEG_LAT
    private fun rows() = kotlin.math.ceil(180 / dLat()).toInt()

    private fun cols(row: Int): Int {
        val latC = -90 + (row + 0.5) * dLat()
        var c = cos(latC * PI / 180)
        if (c < 1e-3) c = 1e-3
        val n = (360 * KM_PER_DEG_LAT * c / CELL_KM).roundToLong()
        return if (n < 1) 1 else n.toInt()
    }

    fun cellOf(latIn: Double, lngIn: Double): Pair<Int, Int> {
        var lat = latIn
        if (lat >= 90) lat = 89.999999
        if (lat < -90) lat = -90.0
        var row = floor((lat + 90) / dLat()).toInt()
        if (row > rows() - 1) row = rows() - 1
        val n = cols(row)
        val lng = ((lngIn + 540) % 360) - 180
        var col = floor((lng + 180) / (360.0 / n)).toInt()
        if (col >= n) col = n - 1
        return row to col
    }
}
