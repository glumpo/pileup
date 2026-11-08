package com.glumpo.pileup.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionsTest {
  private val images = "android.permission.READ_MEDIA_IMAGES"
  private val video = "android.permission.READ_MEDIA_VIDEO"
  private val selected = "android.permission.READ_MEDIA_VISUAL_USER_SELECTED"
  private val storage = "android.permission.READ_EXTERNAL_STORAGE"

  @Test
  fun requestedPermissionsDependOnAndroidVersion() {
    assertEquals(listOf(storage), mediaPermissions(24))
    assertEquals(listOf(storage), mediaPermissions(32))
    assertEquals(listOf(images, video), mediaPermissions(33))
    assertEquals(listOf(images, video, selected), mediaPermissions(34))
    assertEquals(listOf(images, video, selected), mediaPermissions(36))
  }

  @Test
  fun fullAccessIsGranted() {
    assertTrue(mediaGranted(33, setOf(images, video)))
    assertTrue(mediaGranted(34, setOf(images, video, selected)))
    assertTrue(mediaGranted(29, setOf(storage)))
  }

  @Test
  fun partialAccessCounts() {
    assertTrue(mediaGranted(34, setOf(selected)))
  }

  @Test
  fun deniedOrIrrelevantPermissionsDoNot() {
    assertFalse(mediaGranted(34, emptySet()))
    assertFalse(mediaGranted(29, setOf(images)))
    assertFalse(mediaGranted(33, setOf(storage)))
  }
}
