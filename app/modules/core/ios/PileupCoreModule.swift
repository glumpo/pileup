import ExpoModulesCore
import UIKit

public class PileupCoreModule: Module {
  private var termination: NSObjectProtocol?

  public func definition() -> ModuleDefinition {
    Name("PileupCore")

    OnCreate {
      self.termination = NotificationCenter.default.addObserver(forName: UIApplication.willTerminateNotification, object: nil, queue: nil) { _ in
        try? Core.stop()
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

    OnDestroy {
      if let termination = self.termination {
        NotificationCenter.default.removeObserver(termination)
      }
      try? Core.stop()
    }
  }
}
