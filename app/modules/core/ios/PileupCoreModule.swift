import ExpoModulesCore

public class PileupCoreModule: Module {
  private let lifecycle = Lifecycle()

  public func definition() -> ModuleDefinition {
    Name("PileupCore")

    OnCreate {
      self.lifecycle.observe()
    }

    AsyncFunction("start") {
      let support = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
      try Core.start(
        dir: support.appendingPathComponent("core"),
        keystore: KeychainKeystore(service: "com.glumpo.pileup.device"),
        appVersion: Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? ""
      )
    }

    AsyncFunction("stop") {
      try Core.stop()
    }

    Function("port") {
      Core.port
    }

    Function("token") {
      Core.token
    }

    AsyncFunction("requestMediaPermissions") {
      await MediaLibrary.requestPermissions()
    }

    OnDestroy {
      self.lifecycle.unobserve()
      try? Core.stop()
    }
  }
}
