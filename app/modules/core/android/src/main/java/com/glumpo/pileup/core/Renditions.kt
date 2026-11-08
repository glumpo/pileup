package com.glumpo.pileup.core

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.ImageDecoder
import android.graphics.Matrix
import android.media.ExifInterface
import android.media.MediaMetadataRetriever
import android.net.Uri
import android.os.Build
import java.io.ByteArrayOutputStream
import java.io.FileInputStream
import java.io.IOException
import java.nio.ByteBuffer

class Renditions(private val context: Context) : mobile.RenditionMaker {
  private val resolver = context.contentResolver

  override fun rendition(localID: String, tier: Long): ByteArray {
    val side = maxSide(tier)
    val uri = contentUri(localID)
    val bitmap = when (parseLocalId(localID).first) {
      Kind.PHOTO -> if (Build.VERSION.SDK_INT >= 28) decode(uri, side) else decodeLegacy(uri, side)
      Kind.VIDEO -> poster(uri, side)
    }
    val out = ByteArrayOutputStream()
    bitmap.compress(Bitmap.CompressFormat.JPEG, JPEG_QUALITY, out)
    bitmap.recycle()
    return out.toByteArray()
  }

  override fun originalSize(localID: String): Long =
    open(localID).use { it.statSize }

  override fun readOriginal(localID: String, offset: Long, length: Long): ByteArray =
    open(localID).use { fd ->
      FileInputStream(fd.fileDescriptor).channel.use { channel ->
        val buf = ByteBuffer.allocate(readLength(offset, length, channel.size()))
        while (buf.hasRemaining() && channel.read(buf, offset + buf.position()) > 0) Unit
        buf.array().copyOf(buf.position())
      }
    }

  private fun open(localID: String) =
    resolver.openFileDescriptor(contentUri(localID), "r") ?: throw IOException("cannot open $localID")

  private fun decode(uri: Uri, side: Int): Bitmap =
    ImageDecoder.decodeBitmap(ImageDecoder.createSource(resolver, uri)) { decoder, info, _ ->
      val (width, height) = fit(info.size.width, info.size.height, side)
      decoder.setTargetSize(width, height)
      decoder.allocator = ImageDecoder.ALLOCATOR_SOFTWARE
    }

  private fun decodeLegacy(uri: Uri, side: Int): Bitmap {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    resolver.openInputStream(uri).use { BitmapFactory.decodeStream(it, null, bounds) }
    val options = BitmapFactory.Options().apply { inSampleSize = sampleSize(bounds.outWidth, bounds.outHeight, side) }
    val raw = resolver.openInputStream(uri).use { BitmapFactory.decodeStream(it, null, options) }
      ?: throw IOException("cannot decode $uri")
    val orientation = resolver.openInputStream(uri)!!.use {
      ExifInterface(it).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)
    }
    val (rotation, flip) = exifTransform(orientation)
    return transform(raw, side, rotation, flip)
  }

  private fun poster(uri: Uri, side: Int): Bitmap {
    val retriever = MediaMetadataRetriever()
    try {
      retriever.setDataSource(context, uri)
      val frame = retriever.frameAtTime ?: throw IOException("no frame in $uri")
      return transform(frame, side, 0, false)
    } finally {
      retriever.release()
    }
  }

  private fun transform(source: Bitmap, side: Int, rotation: Int, flip: Boolean): Bitmap {
    val (width, height) = fit(source.width, source.height, side)
    if (width == source.width && height == source.height && rotation == 0 && !flip) return source
    val matrix = Matrix()
    matrix.setScale(width.toFloat() / source.width, height.toFloat() / source.height)
    matrix.postRotate(rotation.toFloat())
    if (flip) matrix.postScale(-1f, 1f)
    val result = Bitmap.createBitmap(source, 0, 0, source.width, source.height, matrix, true)
    if (result !== source) source.recycle()
    return result
  }
}
