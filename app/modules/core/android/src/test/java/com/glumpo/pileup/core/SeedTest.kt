package com.glumpo.pileup.core

import javax.crypto.AEADBadTagException
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test

class SeedTest {
  private fun key(): SecretKey = KeyGenerator.getInstance("AES").apply { init(256) }.generateKey()

  private val seed = ByteArray(32) { it.toByte() }

  @Test
  fun sealedSeedOpensWithSameKey() {
    val key = key()
    assertArrayEquals(seed, unseal(key, seal(key, seed)))
  }

  @Test
  fun sealedSeedDoesNotContainPlainSeed() {
    val box = seal(key(), seed)
    assertFalse(box.toList().windowed(seed.size).any { it == seed.toList() })
  }

  @Test
  fun otherKeyCannotOpen() {
    val box = seal(key(), seed)
    assertThrows(AEADBadTagException::class.java) { unseal(key(), box) }
  }

  @Test
  fun tamperedBoxIsRejected() {
    val key = key()
    val box = seal(key, seed)
    box[box.size - 1] = (box[box.size - 1] + 1).toByte()
    assertThrows(AEADBadTagException::class.java) { unseal(key, box) }
  }
}
