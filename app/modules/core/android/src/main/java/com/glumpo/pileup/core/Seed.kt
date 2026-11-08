package com.glumpo.pileup.core

import javax.crypto.Cipher
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

private const val IV_SIZE = 12
private const val TAG_BITS = 128

fun seal(key: SecretKey, seed: ByteArray): ByteArray {
  val cipher = Cipher.getInstance("AES/GCM/NoPadding")
  cipher.init(Cipher.ENCRYPT_MODE, key)
  return cipher.iv + cipher.doFinal(seed)
}

fun unseal(key: SecretKey, box: ByteArray): ByteArray {
  val cipher = Cipher.getInstance("AES/GCM/NoPadding")
  cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(TAG_BITS, box, 0, IV_SIZE))
  return cipher.doFinal(box, IV_SIZE, box.size - IV_SIZE)
}
