package com.glumpo.pileup.core

import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class PileupCoreModule : Module() {
  override fun definition() = ModuleDefinition {
    Name("PileupCore")

    Function("hello") {
      "Hello world! 👋"
    }

    AsyncFunction("setValueAsync") { value: String ->
    }
  }
}
