package com.glumpo.pileup.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class ScaleTest {
  @Test
  fun tiersHaveDocumentedLongSide() {
    assertEquals(256, maxSide(1))
    assertEquals(1920, maxSide(2))
    for (tier in listOf(0L, 3L)) {
      assertThrows(IllegalArgumentException::class.java) { maxSide(tier) }
    }
  }

  @Test
  fun fitLimitsLongSideKeepingAspect() {
    assertEquals(256 to 192, fit(4000, 3000, 256))
    assertEquals(192 to 256, fit(3000, 4000, 256))
    assertEquals(3840 to 2160, fit(7680, 4320, 3840))
    assertEquals(256 to 1, fit(10000, 10, 256))
  }

  @Test
  fun fitNeverUpscales() {
    assertEquals(200 to 100, fit(200, 100, 256))
    assertEquals(256 to 256, fit(256, 256, 256))
  }

  @Test
  fun sampleSizeKeepsDecodedImageAtLeastTarget() {
    assertEquals(1, sampleSize(200, 100, 256))
    assertEquals(1, sampleSize(511, 300, 256))
    assertEquals(2, sampleSize(512, 300, 256))
    assertEquals(8, sampleSize(4000, 3000, 256))
    for ((w, h) in listOf(4000 to 3000, 300 to 9000, 3841 to 10)) {
      val s = sampleSize(w, h, 256)
      assertTrue(maxOf(w, h) / s >= 256)
      assertTrue(maxOf(w, h) / (s * 2) < 256)
    }
  }

  @Test
  fun exifOrientationBecomesRotationThenMirror() {
    assertEquals(0 to false, exifTransform(1))
    assertEquals(0 to true, exifTransform(2))
    assertEquals(180 to false, exifTransform(3))
    assertEquals(180 to true, exifTransform(4))
    assertEquals(90 to true, exifTransform(5))
    assertEquals(90 to false, exifTransform(6))
    assertEquals(270 to true, exifTransform(7))
    assertEquals(270 to false, exifTransform(8))
    assertEquals(0 to false, exifTransform(0))
  }

  @Test
  fun readLengthStaysInsideFile() {
    assertEquals(1024, readLength(0, 1024, 10_000))
    assertEquals(16, readLength(9_984, 1024, 10_000))
    assertEquals(0, readLength(10_000, 1024, 10_000))
    assertEquals(0, readLength(20_000, 1024, 10_000))
    assertEquals(Int.MAX_VALUE, readLength(0, Long.MAX_VALUE, Long.MAX_VALUE))
  }
}
