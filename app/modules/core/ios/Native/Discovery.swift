import Foundation
import Mobile

final class Discovery: NSObject, MobileDiscoveryProviderProtocol {
  func advertise(_ service: String?, name: String?, port: Int, txt: Data?) throws {}

  func browse(_ service: String?, handler: MobileDiscoveryHandlerProtocol?) throws {}

  func stop() throws {}
}
