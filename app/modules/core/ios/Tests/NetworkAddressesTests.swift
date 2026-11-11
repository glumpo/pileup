import XCTest
@testable import PileupNative

final class NetworkAddressesTests: XCTestCase {
  private func decode(_ json: String) throws -> [String] {
    try JSONDecoder().decode([String].self, from: Data(json.utf8))
  }

  func testWifiAndHotspotAddressesOfBothFamiliesAreReported() throws {
    let json = addressesJSON([
      Interface(name: "en0", up: true, loopback: false, address: "192.168.1.20"),
      Interface(name: "en0", up: true, loopback: false, address: "2001:db8::20"),
      Interface(name: "bridge100", up: true, loopback: false, address: "172.20.10.1"),
    ])
    XCTAssertEqual(try decode(json), ["192.168.1.20", "2001:db8::20", "172.20.10.1"])
  }

  func testCellularInterfacesAreExcluded() throws {
    let json = addressesJSON([
      Interface(name: "pdp_ip0", up: true, loopback: false, address: "10.64.1.2"),
      Interface(name: "pdp_ip1", up: true, loopback: false, address: "2a00:1:2::3"),
      Interface(name: "en0", up: true, loopback: false, address: "192.168.1.20"),
    ])
    XCTAssertEqual(try decode(json), ["192.168.1.20"])
  }

  func testLoopbackAndDownInterfacesAreExcluded() throws {
    let json = addressesJSON([
      Interface(name: "lo0", up: true, loopback: true, address: "127.0.0.1"),
      Interface(name: "en1", up: false, loopback: false, address: "192.168.2.5"),
    ])
    XCTAssertEqual(try decode(json), [])
  }

  func testDeviceAddressesAreAJSONArrayOfStrings() throws {
    var error: NSError?
    let json = NetworkAddresses().addrs(&error)
    XCTAssertNil(error)
    XCTAssertFalse(try decode(json).contains("127.0.0.1"))
  }
}
