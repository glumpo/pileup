package com.glumpo.pileup.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class MediaTest {
  private val photo = MediaRecord(
    kind = Kind.PHOTO,
    id = 42,
    dateTaken = 1782986400123,
    dateAdded = 1782986500,
    dateModified = 1782986600,
    width = 4000,
    height = 3000,
    orientation = 0,
    durationMs = null,
    size = 2_500_000,
    mime = "image/heic",
    displayName = "IMG_0001.HEIC",
    relativePath = "DCIM/Camera/",
    bucket = "Camera",
  )

  @Test
  fun photoMatchesCoreMediaItem() {
    assertEquals(
      """[{"local_id":"image:42","kind":"photo","platform_time":1782986400123,"file_time":1782986500000,""" +
        """"width":4000,"height":3000,"size_bytes":2500000,"mime":"image/heic","original_name":"IMG_0001.HEIC",""" +
        """"modified":1782986600000,"screenshot":false,"hidden":false}]""",
      mediaJson(listOf(photo)),
    )
  }

  @Test
  fun videoCarriesDuration() {
    val video = photo.copy(kind = Kind.VIDEO, id = 7, durationMs = 15_250, mime = "video/mp4", displayName = "VID.mp4")
    assertEquals(
      """[{"local_id":"video:7","kind":"video","platform_time":1782986400123,"file_time":1782986500000,""" +
        """"width":4000,"height":3000,"duration_ms":15250,"size_bytes":2500000,"mime":"video/mp4",""" +
        """"original_name":"VID.mp4","modified":1782986600000,"screenshot":false,"hidden":false}]""",
      mediaJson(listOf(video)),
    )
  }

  @Test
  fun missingOrZeroDateTakenIsAbsent() {
    for (taken in listOf(null, 0L)) {
      val json = mediaJson(listOf(photo.copy(dateTaken = taken)))
      assertFalse(json, json.contains("platform_time"))
      assertTrue(json, json.contains("\"file_time\":1782986500000"))
    }
  }

  @Test
  fun missingDateAddedIsAbsent() {
    assertFalse(mediaJson(listOf(photo.copy(dateAdded = null))).contains("file_time"))
  }

  @Test
  fun rotatedPhotoReportsDisplayedSize() {
    for (orientation in listOf(90, 270)) {
      val json = mediaJson(listOf(photo.copy(orientation = orientation)))
      assertTrue(json, json.contains("\"width\":3000,\"height\":4000"))
    }
    assertTrue(mediaJson(listOf(photo.copy(orientation = 180))).contains("\"width\":4000,\"height\":3000"))
  }

  @Test
  fun screenshotIsFlagged() {
    val json = mediaJson(listOf(photo.copy(relativePath = "Pictures/Screenshots/", bucket = "Screenshots")))
    assertTrue(json, json.contains("\"screenshot\":true"))
  }

  @Test
  fun emptyLibraryIsEmptyArray() {
    assertEquals("[]", mediaJson(emptyList()))
  }

  @Test
  fun namesAreEscaped() {
    val json = mediaJson(listOf(photo.copy(displayName = "a\"b\\c\nd\u0001é.jpg")))
    assertTrue(json, json.contains("\"original_name\":\"a\\\"b\\\\c\\u000ad\\u0001é.jpg\""))
  }

  @Test
  fun screenshotsAreRecognisedByFolderOrBucket() {
    assertTrue(isScreenshot("Pictures/Screenshots/", null))
    assertTrue(isScreenshot("DCIM/Screenshots/", "Screenshots"))
    assertTrue(isScreenshot("DCIM/screenshots/", null))
    assertTrue(isScreenshot(null, "Screenshots"))
    assertFalse(isScreenshot("DCIM/Camera/", "Camera"))
    assertFalse(isScreenshot("Pictures/MyScreenshotsArchive/", "MyScreenshotsArchive"))
    assertFalse(isScreenshot(null, null))
  }

  @Test
  fun localIdRoundTrips() {
    for (kind in Kind.entries) {
      assertEquals(kind to 123L, parseLocalId(localId(kind, 123)))
    }
    assertEquals("image:5", localId(Kind.PHOTO, 5))
    assertEquals("video:5", localId(Kind.VIDEO, 5))
  }

  @Test
  fun foreignLocalIdIsRejected() {
    for (bad in listOf("", "5", "audio:5", "image:", "image:x")) {
      assertThrows(IllegalArgumentException::class.java) { parseLocalId(bad) }
    }
  }

  @Test
  fun queryWidensRangeByMargin() {
    val from = 1_782_986_400_000L
    val to = from + 86_400_000L
    val margin = 14L * 60 * 60 * 1000
    val args = mediaQuery(from, to, 34).args.map { it.toLong() }
    assertEquals(listOf(from - margin, to + margin), args.subList(0, 2))
    assertTrue(args[2] * 1000 <= from - margin)
    assertTrue(args[3] * 1000 >= to + margin)
  }

  @Test
  fun queryDoesNotOverflowOnOpenRange() {
    val args = mediaQuery(Long.MIN_VALUE, Long.MAX_VALUE, 34).args.map { it.toLong() }
    assertEquals(listOf(Long.MIN_VALUE, Long.MAX_VALUE), args.subList(0, 2))
    assertTrue(args[2] < 0 && args[3] > 0)
  }

  @Test
  fun stringListIsJsonArray() {
    assertEquals("""["192.168.1.5","fe80::1%wlan0"]""", jsonStrings(listOf("192.168.1.5", "fe80::1%wlan0")))
    assertEquals("[]", jsonStrings(emptyList()))
  }
}
