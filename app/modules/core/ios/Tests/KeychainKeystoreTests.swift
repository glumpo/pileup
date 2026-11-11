import Security
import XCTest
@testable import PileupNative

final class KeychainKeystoreTests: XCTestCase {
  private let service = "test.\(UUID().uuidString)"

  override func tearDownWithError() throws {
    try KeychainKeystore(service: service).reset()
  }

  func testNothingStoredLoadsEmpty() throws {
    XCTAssertEqual(try KeychainKeystore(service: service).load(), Data())
  }

  func testSavedSeedIsLoadedByAnotherInstance() throws {
    let seed = Data((0..<32).map { UInt8($0) })
    try KeychainKeystore(service: service).save(seed)
    XCTAssertEqual(try KeychainKeystore(service: service).load(), seed)
  }

  func testSaveReplacesStoredSeed() throws {
    let keystore = KeychainKeystore(service: service)
    try keystore.save(Data(repeating: 1, count: 32))
    try keystore.save(Data(repeating: 2, count: 32))
    XCTAssertEqual(try keystore.load(), Data(repeating: 2, count: 32))
  }

  func testServicesAreIndependent() throws {
    try KeychainKeystore(service: service).save(Data(repeating: 1, count: 32))
    XCTAssertEqual(try KeychainKeystore(service: service + ".other").load(), Data())
  }

  func testResetForgetsSeed() throws {
    let keystore = KeychainKeystore(service: service)
    try keystore.save(Data(repeating: 1, count: 32))
    try keystore.reset()
    XCTAssertEqual(try keystore.load(), Data())
  }

  func testSeedStaysOnThisDeviceAndIsNotSynced() throws {
    try KeychainKeystore(service: service).save(Data(repeating: 1, count: 32))
    let query: [CFString: Any] = [
      kSecClass: kSecClassGenericPassword,
      kSecAttrService: service,
      kSecAttrSynchronizable: kSecAttrSynchronizableAny,
      kSecReturnAttributes: true,
      kSecMatchLimit: kSecMatchLimitAll,
    ]
    var result: CFTypeRef?
    XCTAssertEqual(SecItemCopyMatching(query as CFDictionary, &result), errSecSuccess)
    let items = try XCTUnwrap(result as? [[String: Any]])
    XCTAssertEqual(items.count, 1)
    XCTAssertEqual(items[0][kSecAttrAccessible as String] as? String, kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)
    XCTAssertNotEqual(items[0][kSecAttrSynchronizable as String] as? Bool, true)
  }
}
