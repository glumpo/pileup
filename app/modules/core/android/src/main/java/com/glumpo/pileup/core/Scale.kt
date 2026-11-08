package com.glumpo.pileup.core

const val JPEG_QUALITY = 85

fun maxSide(tier: Long): Int = when (tier) {
  1L -> 256
  2L -> 3840
  else -> throw IllegalArgumentException("no rendition for tier $tier")
}

fun fit(width: Int, height: Int, maxSide: Int): Pair<Int, Int> {
  val long = maxOf(width, height)
  if (long <= maxSide) return width to height
  val scale = maxSide.toDouble() / long
  return maxOf(1, Math.round(width * scale).toInt()) to maxOf(1, Math.round(height * scale).toInt())
}

fun sampleSize(width: Int, height: Int, maxSide: Int): Int {
  var sample = 1
  while (maxOf(width, height) / (sample * 2) >= maxSide) sample *= 2
  return sample
}

fun exifTransform(orientation: Int): Pair<Int, Boolean> = when (orientation) {
  2 -> 0 to true
  3 -> 180 to false
  4 -> 180 to true
  5 -> 90 to true
  6 -> 90 to false
  7 -> 270 to true
  8 -> 270 to false
  else -> 0 to false
}

fun readLength(offset: Long, length: Long, size: Long): Int =
  minOf(length, size - offset, Int.MAX_VALUE.toLong()).coerceAtLeast(0).toInt()
