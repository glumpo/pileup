package com.glumpo.pileup.core

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import mobile.Mobile

class PileupCoreModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("PileupCore")

    AsyncFunction("start") {
      Mobile.start()
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

    OnActivityDestroys {
      Mobile.stop()
    }

    OnDestroy {
      Mobile.stop()
    }
  }
}
