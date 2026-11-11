import Mobile
import UIKit

final class Lifecycle {
  private var task = UIBackgroundTaskIdentifier.invalid
  private var observers: [NSObjectProtocol] = []

  func observe() {
    let center = NotificationCenter.default
    observers = [
      center.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in self?.background() },
      center.addObserver(forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main) { [weak self] _ in self?.foreground() },
      center.addObserver(forName: UIApplication.willTerminateNotification, object: nil, queue: .main) { _ in try? Core.stop() },
    ]
  }

  func unobserve() {
    observers.forEach(NotificationCenter.default.removeObserver)
    observers = []
  }

  func background() {
    end()
    task = UIApplication.shared.beginBackgroundTask(withName: "pileup") { [weak self] in self?.expired() }
  }

  func expired() {
    do {
      try Core.stop()
    } catch {
      MobileReport("app", "stop on background expiration: \(error)")
    }
    end()
  }

  func foreground() {
    end()
    Core.discovery.restart()
  }

  private func end() {
    if task != .invalid { UIApplication.shared.endBackgroundTask(task) }
    task = .invalid
  }
}
