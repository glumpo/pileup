import Photos
import UIKit

@main
final class TestHost: UIResponder, UIApplicationDelegate {
  var window: UIWindow?

  func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
    guard ProcessInfo.processInfo.environment["PILEUP_REQUEST_PHOTOS"] == "1" else { return true }
    let status = UILabel()
    status.accessibilityIdentifier = "photo-status"
    status.text = "asking"
    let screen = UIViewController()
    screen.view.backgroundColor = .white
    status.frame = CGRect(x: 20, y: 100, width: 300, height: 40)
    screen.view.addSubview(status)
    window = UIWindow(frame: UIScreen.main.bounds)
    window?.rootViewController = screen
    window?.makeKeyAndVisible()
    PHPhotoLibrary.requestAuthorization(for: .readWrite) { answer in
      DispatchQueue.main.async { status.text = "\(answer.rawValue)" }
    }
    return true
  }
}
