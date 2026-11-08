package com.glumpo.pileup.core

fun mediaPermissions(sdk: Int): List<String> = when {
  sdk >= 34 -> listOf(
    "android.permission.READ_MEDIA_IMAGES",
    "android.permission.READ_MEDIA_VIDEO",
    "android.permission.READ_MEDIA_VISUAL_USER_SELECTED",
  )
  sdk >= 33 -> listOf("android.permission.READ_MEDIA_IMAGES", "android.permission.READ_MEDIA_VIDEO")
  else -> listOf("android.permission.READ_EXTERNAL_STORAGE")
}

fun mediaGranted(sdk: Int, granted: Set<String>): Boolean = mediaPermissions(sdk).any { it in granted }
