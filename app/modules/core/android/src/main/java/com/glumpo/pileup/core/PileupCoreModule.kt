package com.glumpo.pileup.core

import android.net.ConnectivityManager
import android.os.Build
import expo.modules.interfaces.permissions.PermissionsStatus
import expo.modules.kotlin.Promise
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.io.File
import mobile.Mobile

class PileupCoreModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("PileupCore")

    AsyncFunction("start") {
      val context = appContext.reactContext?.applicationContext ?: throw Exceptions.ReactContextLost()
      val dir = context.noBackupFilesDir
      Mobile.start(
        File(dir, "core").path,
        "android",
        context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "",
        SeedKeystore(File(dir, "device.seed")),
        MediaStoreProvider(context.contentResolver),
        Renditions(context),
        NetworkAddresses(context.getSystemService(ConnectivityManager::class.java)),
        NsdDiscovery(context),
      )
    }

    AsyncFunction("stop") {
      Mobile.stop()
    }

    Function("port") {
      Mobile.port()
    }

    Function("token") {
      Mobile.token()
    }

    AsyncFunction("requestMediaPermissions") { promise: Promise ->
      val permissions = appContext.permissions ?: throw Exceptions.PermissionsModuleNotFound()
      val sdk = Build.VERSION.SDK_INT
      permissions.askForPermissions(
        { result -> promise.resolve(mediaGranted(sdk, result.filterValues { it.status == PermissionsStatus.GRANTED }.keys)) },
        *mediaPermissions(sdk).toTypedArray(),
      )
    }

    OnActivityDestroys {
      Mobile.stop()
    }

    OnDestroy {
      Mobile.stop()
    }
  }
}
