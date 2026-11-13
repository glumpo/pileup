import Mobile
import Network
import XCTest
@testable import PileupNative

final class Recorder: NSObject, MobileDiscoveryHandlerProtocol {
  struct Found: Equatable {
    let name: String
    let host: String
    let port: Int
    let txt: Data
  }

  private let lock = NSLock()
  private var founds: [Found] = []
  private var losts: [String] = []
  private var access: [Bool] = []

  func found(_ name: String?, host: String?, port: Int, txt: Data?) {
    lock.withLock { founds.append(Found(name: name ?? "", host: host ?? "", port: port, txt: txt ?? Data())) }
  }

  func lost(_ name: String?) {
    lock.withLock { losts.append(name ?? "") }
  }

  func denied(_ denied: Bool) {
    lock.withLock { access.append(denied) }
  }

  func found(_ name: String) -> [Found] { lock.withLock { founds.filter { $0.name == name } } }
  func lost(_ name: String) -> Bool { lock.withLock { losts.contains(name) } }
  var denials: [Bool] { lock.withLock { access } }
}

func eventually(_ seconds: TimeInterval = 20, _ condition: () -> Bool) -> Bool {
  let deadline = Date().addingTimeInterval(seconds)
  while Date() < deadline {
    if condition() { return true }
    RunLoop.current.run(until: Date().addingTimeInterval(0.1))
  }
  return condition()
}

final class DiscoveryTests: XCTestCase {
  private let type = "_pileuptest._tcp"
  private let txt = Data([13]) + Data("pile=0123abcd".utf8) + Data([4]) + Data("flag".utf8)
  private var instance = ""
  private var providers: [Discovery] = []

  override func setUp() {
    instance = "pileup-\(UUID().uuidString.prefix(8).lowercased())"
  }

  override func tearDownWithError() throws {
    try providers.forEach { try $0.stop() }
  }

  private func provider() -> Discovery {
    let d = Discovery()
    providers.append(d)
    return d
  }

  private func browse() throws -> Recorder {
    let seen = Recorder()
    try provider().browse(type, handler: seen)
    return seen
  }

  func testAdvertisedServiceIsFoundWithAddressPortAndTXTAndLostAfterStop() throws {
    let a = provider()
    try a.advertise(type, name: instance, port: 45678, txt: txt)
    let seen = try browse()
    XCTAssertTrue(eventually { !seen.found(instance).isEmpty }, "service not found")
    let found = try XCTUnwrap(seen.found(instance).first)
    XCTAssertNotNil(IPv4Address(found.host), found.host)
    XCTAssertEqual(found.port, 45678)
    XCTAssertEqual(found.txt, txt)
    XCTAssertEqual(seen.denials.last, false)

    try a.stop()
    XCTAssertTrue(eventually { seen.lost(instance) }, "service not lost after stop")
  }

  func testRestartedBrowserFindsTheServiceAgainWithoutLosingIt() throws {
    try provider().advertise(type, name: instance, port: 45679, txt: txt)
    let seen = Recorder()
    let b = provider()
    try b.browse(type, handler: seen)
    XCTAssertTrue(eventually { seen.found(instance).count == 1 })
    b.restart()
    XCTAssertTrue(eventually { seen.found(instance).count == 2 }, "not found again after restart")
    XCTAssertFalse(seen.lost(instance))
  }

  func testRestartedAdvertiserIsStillFound() throws {
    let a = provider()
    try a.advertise(type, name: instance, port: 45680, txt: txt)
    let first = try browse()
    XCTAssertTrue(eventually { !first.found(instance).isEmpty })
    a.restart()
    let later = try browse()
    XCTAssertTrue(eventually { !later.found(instance).isEmpty }, "not advertised after restart")
    XCTAssertEqual(later.found(instance).first?.port, 45680)
  }

  func testStoppedProviderDoesNotComeBackOnRestart() throws {
    let a = provider()
    try a.advertise(type, name: instance, port: 45681, txt: txt)
    let seen = Recorder()
    try a.browse(type, handler: seen)
    XCTAssertTrue(eventually { !seen.found(instance).isEmpty })
    try a.stop()
    a.restart()
    let later = try browse()
    XCTAssertFalse(eventually(3) { !later.found(instance).isEmpty }, "stopped service advertised again")
    XCTAssertEqual(seen.found(instance).count, 1)
  }
}
