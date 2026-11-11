import Foundation
import Mobile

final class MediaLibrary: NSObject, MobileMediaProviderProtocol {
  static func requestPermissions() async -> Bool {
    false
  }

  func media(_ from: Int64, to: Int64) throws -> Data {
    Data("[]".utf8)
  }
}
