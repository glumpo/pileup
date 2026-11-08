package com.glumpo.pileup.core

enum class Kind(val json: String, val prefix: String) {
  PHOTO("photo", "image"),
  VIDEO("video", "video"),
}

data class MediaRecord(
  val kind: Kind,
  val id: Long,
  val dateTaken: Long?,
  val dateAdded: Long?,
  val dateModified: Long?,
  val width: Long,
  val height: Long,
  val orientation: Int,
  val durationMs: Long?,
  val size: Long,
  val mime: String?,
  val displayName: String?,
  val relativePath: String?,
  val bucket: String?,
)

data class MediaQuery(val selection: String, val args: Array<String>)

const val QUERY_MARGIN_MS = 14L * 60 * 60 * 1000

fun localId(kind: Kind, id: Long): String = "${kind.prefix}:$id"

fun parseLocalId(localId: String): Pair<Kind, Long> {
  val kind = Kind.entries.firstOrNull { localId.startsWith(it.prefix + ":") }
  val id = localId.substringAfter(':').toLongOrNull()
  require(kind != null && id != null) { "unknown local id $localId" }
  return kind to id
}

fun isScreenshot(relativePath: String?, bucket: String?): Boolean =
  relativePath.orEmpty().split('/').any { it.equals("Screenshots", ignoreCase = true) } ||
    bucket.equals("Screenshots", ignoreCase = true)

fun mediaQuery(from: Long, to: Long, sdk: Int): MediaQuery {
  val start = if (from < Long.MIN_VALUE + QUERY_MARGIN_MS) Long.MIN_VALUE else from - QUERY_MARGIN_MS
  val end = if (to > Long.MAX_VALUE - QUERY_MARGIN_MS) Long.MAX_VALUE else to + QUERY_MARGIN_MS
  var selection = "(datetaken BETWEEN ? AND ? OR (datetaken IS NULL OR datetaken <= 0) AND (date_added BETWEEN ? AND ?))"
  if (sdk >= 29) selection += " AND is_pending = 0"
  if (sdk >= 30) selection += " AND is_trashed = 0"
  val args = arrayOf(start, end, Math.floorDiv(start, 1000L), Math.floorDiv(end, 1000L) + 1).map { it.toString() }
  return MediaQuery(selection, args.toTypedArray())
}

fun mediaJson(records: List<MediaRecord>): String =
  records.joinToString(",", "[", "]") { r ->
    val turned = r.orientation == 90 || r.orientation == 270
    buildList {
      add("\"local_id\":" + jsonString(localId(r.kind, r.id)))
      add("\"kind\":" + jsonString(r.kind.json))
      r.dateTaken?.takeIf { it > 0 }?.let { add("\"platform_time\":$it") }
      r.dateAdded?.let { add("\"file_time\":${it * 1000}") }
      add("\"width\":${if (turned) r.height else r.width}")
      add("\"height\":${if (turned) r.width else r.height}")
      r.durationMs?.let { add("\"duration_ms\":$it") }
      add("\"size_bytes\":${r.size}")
      add("\"mime\":" + jsonString(r.mime.orEmpty()))
      add("\"original_name\":" + jsonString(r.displayName.orEmpty()))
      add("\"modified\":${(r.dateModified ?: 0) * 1000}")
      add("\"screenshot\":${isScreenshot(r.relativePath, r.bucket)}")
      add("\"hidden\":false")
    }.joinToString(",", "{", "}")
  }

fun jsonStrings(values: List<String>): String = values.joinToString(",", "[", "]") { jsonString(it) }

fun jsonString(s: String): String = buildString {
  append('"')
  for (c in s) {
    when {
      c == '"' -> append("\\\"")
      c == '\\' -> append("\\\\")
      c < ' ' -> append("\\u%04x".format(c.code))
      else -> append(c)
    }
  }
  append('"')
}
