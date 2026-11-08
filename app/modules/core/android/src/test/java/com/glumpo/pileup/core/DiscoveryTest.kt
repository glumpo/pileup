package com.glumpo.pileup.core

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

class DiscoveryTest {
  private fun record(vararg entries: String): ByteArray =
    entries.flatMap { listOf(it.toByteArray().size.toByte()) + it.toByteArray().toList() }.toByteArray()

  @Test
  fun txtRecordIsDnsSdWireFormat() {
    assertArrayEquals(
      record("pile=0123abcd", "fn=Гостиная", "flag"),
      txtRecord(linkedMapOf("pile" to "0123abcd".toByteArray(), "fn" to "Гостиная".toByteArray(), "flag" to null)),
    )
  }

  @Test
  fun txtEntryLongerThan255BytesIsSkipped() {
    assertArrayEquals(record("a=1"), txtRecord(linkedMapOf("long" to ByteArray(252), "a" to "1".toByteArray())))
  }

  @Test
  fun txtAttributesSplitAtFirstEquals() {
    assertEquals(
      listOf("pile" to "0123abcd", "fn" to "a=b", "flag" to ""),
      txtAttributes(record("pile=0123abcd", "fn=a=b", "flag")),
    )
  }

  @Test
  fun truncatedTxtRecordKeepsCompleteEntries() {
    assertEquals(listOf("pile" to "01"), txtAttributes(record("pile=01") + byteArrayOf(9, 'x'.code.toByte())))
    assertEquals(emptyList<Pair<String, String>>(), txtAttributes(ByteArray(0)))
  }

  @Test
  fun lockIsHeldFromFirstBeginUntilEnd() {
    val events = mutableListOf<String>()
    val session = Session({ events.add("acquire") }, { events.add("release") })
    session.begin { events.add("unregister") }
    session.begin { events.add("stop browsing") }
    assertEquals(listOf("acquire"), events)
    session.end()
    assertEquals(listOf("acquire", "unregister", "stop browsing", "release"), events)
  }

  @Test
  fun failingStopStillReleasesLock() {
    val events = mutableListOf<String>()
    val session = Session({ events.add("acquire") }, { events.add("release") })
    session.begin { throw IllegalArgumentException("listener not registered") }
    session.begin { events.add("stop browsing") }
    session.end()
    assertEquals(listOf("acquire", "stop browsing", "release"), events)
  }

  @Test
  fun lockIsAcquiredAgainAfterRestart() {
    val events = mutableListOf<String>()
    val session = Session({ events.add("acquire") }, { events.add("release") })
    session.end()
    session.begin {}
    session.end()
    session.begin {}
    assertEquals(listOf("acquire", "release", "acquire"), events)
  }

  @Test
  fun queueRunsOneItemAtATimeInOrder() {
    val started = mutableListOf<Int>()
    val finish = mutableListOf<() -> Unit>()
    val queue = SerialQueue<Int> { item, done ->
      started.add(item)
      finish.add(done)
    }
    queue.add(1)
    queue.add(2)
    queue.add(3)
    assertEquals(listOf(1), started)
    finish[0]()
    assertEquals(listOf(1, 2), started)
    finish[1]()
    finish[2]()
    queue.add(4)
    assertEquals(listOf(1, 2, 3, 4), started)
  }

  @Test
  fun queueContinuesWhenItemFinishesImmediately() {
    val started = mutableListOf<Int>()
    val queue = SerialQueue<Int> { item, done ->
      started.add(item)
      done()
    }
    queue.add(1)
    queue.add(2)
    assertEquals(listOf(1, 2), started)
  }

  @Test
  fun clearedQueueDropsPendingItems() {
    val started = mutableListOf<Int>()
    val finish = mutableListOf<() -> Unit>()
    val queue = SerialQueue<Int> { item, done ->
      started.add(item)
      finish.add(done)
    }
    queue.add(1)
    queue.add(2)
    queue.clear()
    finish[0]()
    queue.add(3)
    assertEquals(listOf(1, 3), started)
  }
}
