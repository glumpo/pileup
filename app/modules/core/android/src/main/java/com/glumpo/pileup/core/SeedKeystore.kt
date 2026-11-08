package com.glumpo.pileup.core

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.security.KeyStore
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey

class SeedKeystore(private val file: File) : mobile.Keystore {
  override fun load(): ByteArray = if (file.exists()) unseal(key(), file.readBytes()) else ByteArray(0)

  override fun save(seed: ByteArray) {
    val tmp = File(file.path + ".tmp")
    FileOutputStream(tmp).use {
      it.write(seal(key(), seed))
      it.fd.sync()
    }
    if (!tmp.renameTo(file)) throw IOException("cannot store device key")
  }

  private fun key(): SecretKey {
    val store = KeyStore.getInstance(PROVIDER).apply { load(null) }
    (store.getKey(ALIAS, null) as SecretKey?)?.let { return it }
    val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER)
    generator.init(
      KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
        .setKeySize(256)
        .build()
    )
    return generator.generateKey()
  }

  private companion object {
    const val PROVIDER = "AndroidKeyStore"
    const val ALIAS = "pileup-device-seed"
  }
}
