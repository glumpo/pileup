import ExpoModulesCore

public class PileupCoreModule: Module {
  private let lifecycle = Lifecycle()

  public func definition() -> ModuleDefinition {
    Name("PileupCore")

    Events("onExternalDisplay", "onPlayer")

    OnCreate {
      self.lifecycle.observe()
      DispatchQueue.main.async {
        Presentation.shared.onDisplay = { [weak self] connected in
          self?.sendEvent("onExternalDisplay", ["connected": connected])
        }
        Presentation.shared.onPlayer = { [weak self] asset, state, message in
          self?.sendEvent("onPlayer", ["asset": asset, "state": state, "message": message])
        }
      }
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

    AsyncFunction("present") { (slide: [String: Any]?) in
      Presentation.shared.show(Slide(slide))
    }.runOnQueue(.main)

    AsyncFunction("externalDisplay") {
      Presentation.shared.connected
    }.runOnQueue(.main)

    OnDestroy {
      self.lifecycle.unobserve()
      DispatchQueue.main.async { Presentation.shared.show(nil) }
      try? Core.stop()
    }
  }
}
