import Foundation
import Mobile
import Security

final class KeychainKeystore: NSObject, MobileKeystoreProtocol {
  private let query: [CFString: Any]

  init(service: String) {
    query = [kSecClass: kSecClassGenericPassword, kSecAttrService: service, kSecAttrAccount: "device-seed"]
  }

  func load() throws -> Data {
    var request = query
    request[kSecReturnData] = true
    var result: CFTypeRef?
    let status = SecItemCopyMatching(request as CFDictionary, &result)
    if status == errSecItemNotFound { return Data() }
    try check(status)
    return result as? Data ?? Data()
  }

  func save(_ seed: Data?) throws {
    try reset()
    var item = query
    item[kSecValueData] = seed ?? Data()
    item[kSecAttrAccessible] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    try check(SecItemAdd(item as CFDictionary, nil))
  }

  func reset() throws {
    let status = SecItemDelete(query as CFDictionary)
    if status != errSecItemNotFound { try check(status) }
  }
}

private func check(_ status: OSStatus) throws {
  if status != errSecSuccess {
    throw NSError(domain: NSOSStatusErrorDomain, code: Int(status), userInfo: [NSLocalizedDescriptionKey: "keychain error \(status)"])
  }
}
