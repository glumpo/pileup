import XCTest
@testable import PileupNative

final class CoreTests: XCTestCase {
  private struct Member: Decodable {
    let key: String
    let name: String
    let platform: String
    let app_version: String
  }

  private struct Pile: Decodable {
    let id: String
    let members: [Member]
  }

  private let keystore = KeychainKeystore(service: "test.\(UUID().uuidString)")
  private var dir: URL!

  override func setUpWithError() throws {
    dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("core")
  }

  override func tearDownWithError() throws {
    try Core.stop()
    try keystore.reset()
    try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
  }

  private func start() throws {
    try Core.start(dir: dir, keystore: keystore, appVersion: "1.2.3")
  }

  private func request(_ path: String, token: String? = Core.token, method: String = "GET", body: String? = nil) async throws -> (Int, Data) {
    var request = URLRequest(url: URL(string: "http://127.0.0.1:\(Core.port)\(path)")!)
    request.httpMethod = method
    if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
    if let body {
      request.httpBody = Data(body.utf8)
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    }
    let (data, response) = try await URLSession.shared.data(for: request)
    return ((response as! HTTPURLResponse).statusCode, data)
  }

  func testHealthAnswersOnlyWithToken() async throws {
    try start()
    XCTAssertGreaterThan(Core.port, 0)
    XCTAssertFalse(Core.token.isEmpty)
    let (ok, _) = try await request("/health")
    XCTAssertEqual(ok, 200)
    let (missing, _) = try await request("/health", token: nil)
    XCTAssertEqual(missing, 401)
    let (wrong, _) = try await request("/health", token: "wrong")
    XCTAssertEqual(wrong, 401)
  }

  func testMemberIsAnIOSDeviceWithAppVersion() async throws {
    try start()
    let (status, data) = try await request("/piles", method: "POST", body: #"{"name":"Trip","member":"Phone"}"#)
    XCTAssertEqual(status, 201)
    let pile = try JSONDecoder().decode(Pile.self, from: data)
    XCTAssertEqual(pile.members.map(\.platform), ["ios"])
    XCTAssertEqual(pile.members.map(\.app_version), ["1.2.3"])
  }

  func testInviteCarriesDeviceAddresses() async throws {
    try start()
    let (_, data) = try await request("/piles", method: "POST", body: #"{"name":"Trip","member":"Phone"}"#)
    let pile = try JSONDecoder().decode(Pile.self, from: data)
    let (status, _) = try await request("/piles/\(pile.id)/invite")
    XCTAssertEqual(status, 200)
  }

  func testStopClosesPortAndRestartKeepsDeviceAndPiles() async throws {
    try start()
    let port = Core.port
    let token = Core.token
    let (_, data) = try await request("/piles", method: "POST", body: #"{"name":"Trip","member":"Phone"}"#)
    let created = try JSONDecoder().decode(Pile.self, from: data)
    try Core.stop()
    XCTAssertEqual(Core.port, 0)
    var closed = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/health")!)
    closed.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    do {
      _ = try await URLSession.shared.data(for: closed)
      XCTFail("stopped core still answers")
    } catch {}

    try start()
    XCTAssertNotEqual(Core.token, token)
    let (stale, _) = try await request("/health", token: token)
    XCTAssertEqual(stale, 401)
    let (status, list) = try await request("/piles")
    XCTAssertEqual(status, 200)
    let piles = try JSONDecoder().decode([Pile].self, from: list)
    XCTAssertEqual(piles.map(\.id), [created.id])
    XCTAssertEqual(piles[0].members.map(\.key), created.members.map(\.key))
  }
}
