import Network
import XCTest
import dnssd
@testable import PileupNative

final class DiscoveryLogicTests: XCTestCase {
  private func service(_ name: String, domain: String = "local.") -> NWEndpoint {
    .service(name: name, type: "_pileup._tcp", domain: domain, interface: nil)
  }

  func testNewServicesAreResolvedOncePerName() {
    var results = Results()
    let first = results.update([service("a"), service("b"), service("a", domain: "other.")], changed: [])
    XCTAssertEqual(first.resolve.compactMap(serviceName).sorted(), ["a", "b"])
    XCTAssertEqual(first.lost, [])
    let again = results.update([service("a"), service("b")], changed: [])
    XCTAssertEqual(again.resolve, [])
    XCTAssertEqual(again.lost, [])
  }

  func testServiceIsLostWhenItsLastResultIsGone() {
    var results = Results()
    _ = results.update([service("a"), service("a", domain: "other."), service("b")], changed: [])
    let one = results.update([service("a", domain: "other."), service("b")], changed: [])
    XCTAssertEqual(one.lost, [])
    let gone = results.update([service("b")], changed: [])
    XCTAssertEqual(gone.lost, ["a"])
    XCTAssertEqual(gone.resolve, [])
  }

  func testChangedTXTIsResolvedAgain() {
    var results = Results()
    _ = results.update([service("a"), service("b")], changed: [])
    let changed = results.update([service("a"), service("b")], changed: [service("b")])
    XCTAssertEqual(changed.resolve.compactMap(serviceName), ["b"])
  }

  func testAfterRestartPresentServicesAreResolvedAgainAndVanishedOnesLost() {
    var results = Results()
    _ = results.update([service("a"), service("b")], changed: [])
    results.restart()
    let first = results.update([service("a")], changed: [])
    XCTAssertEqual(first.resolve.compactMap(serviceName), ["a"])
    XCTAssertEqual(first.lost, ["b"])
    let next = results.update([service("a"), service("c")], changed: [])
    XCTAssertEqual(next.resolve.compactMap(serviceName), ["c"])
    XCTAssertEqual(next.lost, [])
  }

  func testPolicyDeniedMeansNoLocalNetworkAccessAndReadyMeansAccess() {
    let denied = NWError.dns(DNSServiceErrorType(kDNSServiceErr_PolicyDenied))
    XCTAssertEqual(localNetworkDenied(.waiting(denied)), true)
    XCTAssertEqual(localNetworkDenied(.failed(denied)), true)
    XCTAssertEqual(localNetworkDenied(.ready), false)
    XCTAssertNil(localNetworkDenied(.waiting(.posix(.ENETDOWN))))
    XCTAssertNil(localNetworkDenied(.failed(.dns(DNSServiceErrorType(kDNSServiceErr_ServiceNotRunning)))))
    XCTAssertNil(localNetworkDenied(.setup))
    XCTAssertNil(localNetworkDenied(.cancelled))
  }
}
