import Foundation
import Mobile

final class Renditions: NSObject, MobileRenditionMakerProtocol {
  func rendition(_ localID: String?, tier: Int) throws -> Data {
    throw unavailable
  }

  func originalSize(_ localID: String?, ret0_: UnsafeMutablePointer<Int64>?) throws {
    throw unavailable
  }

  func readOriginal(_ localID: String?, offset: Int64, length: Int64) throws -> Data {
    throw unavailable
  }
}

private let unavailable = NSError(domain: "pileup", code: 1, userInfo: [NSLocalizedDescriptionKey: "media library is not available on iOS yet"])
