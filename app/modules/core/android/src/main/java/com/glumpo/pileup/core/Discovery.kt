package com.glumpo.pileup.core

import java.io.ByteArrayOutputStream

fun txtRecord(attributes: Map<String, ByteArray?>): ByteArray {
  val out = ByteArrayOutputStream()
  for ((key, value) in attributes) {
    val entry = key.toByteArray() + (value?.let { "=".toByteArray() + it } ?: ByteArray(0))
    if (entry.size > 255) continue
    out.write(entry.size)
    out.write(entry)
  }
  return out.toByteArray()
}

fun txtAttributes(record: ByteArray): List<Pair<String, String>> {
  val out = mutableListOf<Pair<String, String>>()
  var i = 0
  while (i < record.size) {
    val size = record[i].toInt() and 0xff
    if (i + 1 + size > record.size) break
    val entry = String(record, i + 1, size, Charsets.UTF_8)
    out.add(entry.substringBefore('=') to entry.substringAfter('=', ""))
    i += 1 + size
  }
  return out
}

class Session(private val acquire: () -> Unit, private val release: () -> Unit) {
  private val stops = mutableListOf<() -> Unit>()

  fun begin(stop: () -> Unit) = synchronized(this) {
    if (stops.isEmpty()) acquire()
    stops.add(stop)
  }

  fun end() {
    val all = synchronized(this) { stops.toList().also { stops.clear() } }
    all.forEach { runCatching(it) }
    if (all.isNotEmpty()) release()
  }
}

class SerialQueue<T>(private val run: (T, () -> Unit) -> Unit) {
  private val pending = ArrayDeque<T>()
  private var busy = false

  fun add(item: T) {
    synchronized(this) {
      if (busy) {
        pending.addLast(item)
        return
      }
      busy = true
    }
    run(item, ::next)
  }

  fun clear() = synchronized(this) { pending.clear() }

  private fun next() {
    val item = synchronized(this) { pending.removeFirstOrNull().also { if (it == null) busy = false } } ?: return
    run(item, ::next)
  }
}
