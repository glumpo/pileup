import XCTest
@testable import PileupNative

final class LifecycleTests: XCTestCase {
  private let keystore = KeychainKeystore(service: "test.\(UUID().uuidString)")
  private let lifecycle = Lifecycle()
  private var dir: URL!

  override func setUpWithError() throws {
    dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("core")
    try Core.start(dir: dir, keystore: keystore, appVersion: "1.2.3")
  }

  override func tearDownWithError() throws {
    try Core.stop()
    try keystore.reset()
    try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
  }

  private func health(_ port: Int) async -> Int? {
    var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/health")!)
    request.setValue("Bearer \(Core.token)", forHTTPHeaderField: "Authorization")
    request.timeoutInterval = 5
    let response = try? await URLSession.shared.data(for: request).1
    return (response as? HTTPURLResponse)?.statusCode
  }

  func testShortBackgroundKeepsTheCoreRunning() async throws {
    let port = Core.port
    await MainActor.run {
      lifecycle.background()
      lifecycle.foreground()
    }
    XCTAssertEqual(Core.port, port)
    let status = await health(port)
    XCTAssertEqual(status, 200)
  }

  func testExpiredBackgroundTimeStopsTheCoreAndForegroundLeavesTheRestartToTheApp() async throws {
    let port = Core.port
    await MainActor.run {
      lifecycle.background()
      lifecycle.expired()
    }
    XCTAssertEqual(Core.port, 0)
    let stopped = await health(port)
    XCTAssertNil(stopped, "core still answers after the background time expired")

    await MainActor.run { lifecycle.foreground() }
    XCTAssertEqual(Core.port, 0)
  }

  func testExpiredBackgroundTimeIsInTheJournalWithTheStopDuration() async throws {
    await MainActor.run {
      lifecycle.background()
      lifecycle.expired()
    }
    let journal = dir.appendingPathComponent("diag.log")
    XCTAssertTrue(try String(contentsOf: journal, encoding: .utf8).contains("background time expired, stopping the core"))
    try Core.start(dir: dir, keystore: keystore, appVersion: "1.2.3")
    let stopped = try String(contentsOf: journal, encoding: .utf8).components(separatedBy: "\n").first { $0.contains("core stopped after background expiration in ") }
    XCTAssertNotNil(stopped?.range(of: #"in \d+ ms"#, options: .regularExpression), "\(String(describing: stopped))")
  }
}
