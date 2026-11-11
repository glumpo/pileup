import XCTest
@testable import PileupNative

final class DataDirectoryTests: XCTestCase {
  private let keystore = KeychainKeystore(service: "test.\(UUID().uuidString)")
  private var dir: URL!

  override func setUpWithError() throws {
    dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("core")
  }

  override func tearDownWithError() throws {
    try keystore.reset()
    try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
  }

  private func excludedFromBackup() throws -> Bool {
    try XCTUnwrap(dir.resourceValues(forKeys: [.isExcludedFromBackupKey]).isExcludedFromBackup)
  }

  func testNewDirectoryIsCreatedAndExcludedFromBackup() throws {
    try prepareDataDirectory(dir, keystore: keystore)
    var isDirectory: ObjCBool = false
    XCTAssertTrue(FileManager.default.fileExists(atPath: dir.path, isDirectory: &isDirectory))
    XCTAssertTrue(isDirectory.boolValue)
    XCTAssertTrue(try excludedFromBackup())
  }

  func testSeedWithoutDataDirectoryIsDiscarded() throws {
    try keystore.save(Data(repeating: 1, count: 32))
    try prepareDataDirectory(dir, keystore: keystore)
    XCTAssertEqual(try keystore.load(), Data())
  }

  func testSeedIsKeptWithExistingDataDirectory() throws {
    try prepareDataDirectory(dir, keystore: keystore)
    try keystore.save(Data(repeating: 1, count: 32))
    try prepareDataDirectory(dir, keystore: keystore)
    XCTAssertEqual(try keystore.load(), Data(repeating: 1, count: 32))
    XCTAssertTrue(try excludedFromBackup())
  }
}
