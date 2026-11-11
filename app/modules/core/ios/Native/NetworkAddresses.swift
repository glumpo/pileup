import Darwin
import Foundation
import Mobile

struct Interface {
  let name: String
  let up: Bool
  let loopback: Bool
  let address: String
}

func addressesJSON(_ interfaces: [Interface]) -> String {
  let addresses = interfaces.filter { $0.up && !$0.loopback && !$0.name.hasPrefix("pdp_ip") }.map(\.address)
  return String(decoding: try! JSONEncoder().encode(addresses), as: UTF8.self)
}

final class NetworkAddresses: NSObject, MobileNetworkInfoProtocol {
  func addrs(_ error: NSErrorPointer) -> String {
    addressesJSON(interfaces())
  }
}

private func interfaces() -> [Interface] {
  var head: UnsafeMutablePointer<ifaddrs>?
  guard getifaddrs(&head) == 0 else { return [] }
  defer { freeifaddrs(head) }
  var out: [Interface] = []
  var next = head
  while let ifa = next {
    next = ifa.pointee.ifa_next
    guard let sa = ifa.pointee.ifa_addr, sa.pointee.sa_family == AF_INET || sa.pointee.sa_family == AF_INET6 else { continue }
    var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
    guard getnameinfo(sa, socklen_t(sa.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 else { continue }
    let flags = Int32(ifa.pointee.ifa_flags)
    out.append(Interface(
      name: String(cString: ifa.pointee.ifa_name),
      up: flags & IFF_UP != 0,
      loopback: flags & IFF_LOOPBACK != 0,
      address: String(cString: host)
    ))
  }
  return out
}
