package com.glumpo.pileup.core

import android.content.ContentResolver
import android.content.ContentUris
import android.database.Cursor
import android.net.Uri
import android.os.Build
import android.provider.BaseColumns
import android.provider.MediaStore
import android.provider.MediaStore.MediaColumns

fun collection(kind: Kind): Uri = when (kind) {
  Kind.PHOTO -> MediaStore.Images.Media.EXTERNAL_CONTENT_URI
  Kind.VIDEO -> MediaStore.Video.Media.EXTERNAL_CONTENT_URI
}

fun contentUri(localId: String): Uri {
  val (kind, id) = parseLocalId(localId)
  return ContentUris.withAppendedId(collection(kind), id)
}

class MediaStoreProvider(private val resolver: ContentResolver) : mobile.MediaProvider {
  override fun media(from: Long, to: Long): ByteArray {
    val query = mediaQuery(from, to, Build.VERSION.SDK_INT)
    return mediaJson(Kind.entries.flatMap { records(it, query) }).toByteArray()
  }

  private fun records(kind: Kind, query: MediaQuery): List<MediaRecord> {
    val sdk = Build.VERSION.SDK_INT
    val columns = mutableListOf(
      BaseColumns._ID, DATE_TAKEN, MediaColumns.DATE_ADDED, MediaColumns.DATE_MODIFIED, MediaColumns.WIDTH,
      MediaColumns.HEIGHT, MediaColumns.SIZE, MediaColumns.MIME_TYPE, MediaColumns.DISPLAY_NAME, BUCKET_DISPLAY_NAME,
    )
    if (kind == Kind.VIDEO) columns += DURATION
    if (kind == Kind.PHOTO || sdk >= 29) columns += ORIENTATION
    if (sdk >= 29) columns += MediaColumns.RELATIVE_PATH
    val cursor = resolver.query(collection(kind), columns.toTypedArray(), query.selection, query.args, null)
    return cursor?.use { c ->
      buildList {
        while (c.moveToNext()) {
          add(
            MediaRecord(
              kind = kind,
              id = c.getLong(c.getColumnIndexOrThrow(BaseColumns._ID)),
              dateTaken = c.long(DATE_TAKEN),
              dateAdded = c.long(MediaColumns.DATE_ADDED),
              dateModified = c.long(MediaColumns.DATE_MODIFIED),
              width = c.long(MediaColumns.WIDTH) ?: 0,
              height = c.long(MediaColumns.HEIGHT) ?: 0,
              orientation = c.long(ORIENTATION)?.toInt() ?: 0,
              durationMs = c.long(DURATION),
              size = c.long(MediaColumns.SIZE) ?: 0,
              mime = c.string(MediaColumns.MIME_TYPE),
              displayName = c.string(MediaColumns.DISPLAY_NAME),
              relativePath = c.string(MediaColumns.RELATIVE_PATH),
              bucket = c.string(BUCKET_DISPLAY_NAME),
            )
          )
        }
      }
    }.orEmpty()
  }

  private fun Cursor.long(column: String): Long? =
    getColumnIndex(column).takeIf { it >= 0 && !isNull(it) }?.let { getLong(it) }

  private fun Cursor.string(column: String): String? =
    getColumnIndex(column).takeIf { it >= 0 && !isNull(it) }?.let { getString(it) }

  private companion object {
    const val DATE_TAKEN = "datetaken"
    const val DURATION = "duration"
    const val ORIENTATION = "orientation"
    const val BUCKET_DISPLAY_NAME = "bucket_display_name"
  }
}
