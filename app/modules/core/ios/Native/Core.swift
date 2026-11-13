import Foundation
import Mobile

enum Core {
  static let discovery = Discovery()

  static func start(dir: URL, keystore: KeychainKeystore, appVersion: String) throws {
    try prepareDataDirectory(dir, keystore: keystore)
    var error: NSError?
    guard MobileStart(dir.path, "ios", appVersion, keystore, MediaLibrary(), Renditions(), NetworkAddresses(), discovery, &error) else { throw error! }
  }

  static func stop() throws {
    var error: NSError?
    guard MobileStop(&error) else { throw error! }
  }

  static var port: Int { MobilePort() }

  static var token: String { MobileToken() }
}

func prepareDataDirectory(_ dir: URL, keystore: KeychainKeystore) throws {
  if !FileManager.default.fileExists(atPath: dir.path) {
    try keystore.reset()
  }
  try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
  var values = URLResourceValues()
  values.isExcludedFromBackup = true
  var url = dir
  try url.setResourceValues(values)
}
