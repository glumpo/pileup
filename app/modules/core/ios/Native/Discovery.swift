import Darwin
import Foundation
import Mobile
import Network
import dnssd

private let noError = DNSServiceErrorType(kDNSServiceErr_NoError)

private func report(_ message: String) {
  MobileReport("discovery", "dns-sd: \(message)")
}

func localNetworkDenied(_ state: NWBrowser.State) -> Bool? {
  switch state {
  case .ready:
    return false
  case .waiting(let error), .failed(let error):
    if case .dns(let code) = error, code == DNSServiceErrorType(kDNSServiceErr_PolicyDenied) { return true }
    return nil
  default:
    return nil
  }
}

func serviceName(_ endpoint: NWEndpoint) -> String? {
  if case let .service(name, _, _, _) = endpoint { return name }
  return nil
}

struct Results {
  private(set) var known: Set<String> = []
  private var stale: Set<String> = []

  mutating func restart() {
    stale.formUnion(known)
    known = []
  }

  mutating func update(_ now: [NWEndpoint], changed: [NWEndpoint]) -> (resolve: [NWEndpoint], lost: [String]) {
    let again = Set(changed.compactMap(serviceName))
    var names: Set<String> = []
    var resolve: [NWEndpoint] = []
    for endpoint in now {
      guard let name = serviceName(endpoint), names.insert(name).inserted else { continue }
      if !known.contains(name) || again.contains(name) { resolve.append(endpoint) }
    }
    let lost = known.union(stale).subtracting(names).sorted()
    known = names
    stale = []
    return (resolve, lost)
  }
}

func numericHost(_ address: UnsafePointer<sockaddr>) -> String? {
  var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
  guard getnameinfo(address, socklen_t(address.pointee.sa_len), &host, socklen_t(host.count), nil, 0, NI_NUMERICHOST) == 0 else { return nil }
  return String(cString: host)
}

private final class Lookup {
  private let queue: DispatchQueue
  private let what: String
  private let done: (Lookup, (host: String, port: Int, txt: Data)?) -> Void
  private var ref: DNSServiceRef?
  private var finished = false
  private var port = 0
  private var txt = Data()

  init(queue: DispatchQueue, what: String, done: @escaping (Lookup, (host: String, port: Int, txt: Data)?) -> Void) {
    self.queue = queue
    self.what = what
    self.done = done
  }

  func start(_ endpoint: NWEndpoint) {
    guard case let .service(name, type, domain, interface) = endpoint else { return finish(nil) }
    let status = DNSServiceResolve(&ref, 0, UInt32(interface?.index ?? 0), name, type, domain, { _, _, index, error, _, host, port, length, txt, context in
      let record = txt.map { Data(bytes: $0, count: Int(length)) } ?? Data()
      Unmanaged<Lookup>.fromOpaque(context!).takeUnretainedValue().resolved(error, index, host, Int(UInt16(bigEndian: port)), record)
    }, Unmanaged.passUnretained(self).toOpaque())
    began(status, "resolve")
    queue.asyncAfter(deadline: .now() + 10) { [weak self] in
      guard let self, !self.finished else { return }
      self.fail("timed out")
    }
  }

  func cancel() {
    if let ref { DNSServiceRefDeallocate(ref) }
    ref = nil
    finished = true
  }

  private func began(_ status: DNSServiceErrorType, _ step: String) {
    guard status == noError, let ref else { return fail("\(step) failed, error \(status)") }
    DNSServiceSetDispatchQueue(ref, queue)
  }

  private func resolved(_ error: DNSServiceErrorType, _ index: UInt32, _ host: UnsafePointer<CChar>?, _ port: Int, _ txt: Data) {
    if let ref { DNSServiceRefDeallocate(ref) }
    ref = nil
    guard error == noError, let host else { return fail("resolve failed, error \(error)") }
    self.port = port
    self.txt = txt
    let status = DNSServiceGetAddrInfo(&ref, 0, index, DNSServiceProtocol(kDNSServiceProtocol_IPv4), host, { _, flags, _, error, _, address, _, context in
      Unmanaged<Lookup>.fromOpaque(context!).takeUnretainedValue().addressed(error, flags, address)
    }, Unmanaged.passUnretained(self).toOpaque())
    began(status, "address lookup")
  }

  private func addressed(_ error: DNSServiceErrorType, _ flags: DNSServiceFlags, _ address: UnsafePointer<sockaddr>?) {
    guard error == noError else { return fail("address lookup failed, error \(error)") }
    guard flags & DNSServiceFlags(kDNSServiceFlagsAdd) != 0, let address, let host = numericHost(address) else { return }
    finish((host, port, txt))
  }

  private func fail(_ message: String) {
    report("\(what): \(message)")
    finish(nil)
  }

  private func finish(_ found: (host: String, port: Int, txt: Data)?) {
    guard !finished else { return }
    cancel()
    done(self, found)
  }
}

final class Discovery: NSObject, MobileDiscoveryProviderProtocol {
  private struct Advert {
    let service: String
    let name: String
    let port: Int
    let txt: Data
  }

  private final class Search {
    let service: String
    let handler: MobileDiscoveryHandlerProtocol
    var results = Results()
    var browser: NWBrowser?
    var lookups: [String: Lookup] = [:]

    init(service: String, handler: MobileDiscoveryHandlerProtocol) {
      self.service = service
      self.handler = handler
    }
  }

  private let queue = DispatchQueue(label: "com.glumpo.pileup.discovery")
  private var adverts: [Advert] = []
  private var registrations: [DNSServiceRef] = []
  private var searches: [Search] = []

  func advertise(_ service: String?, name: String?, port: Int, txt: Data?) throws {
    let advert = Advert(service: service ?? "", name: name ?? "", port: port, txt: txt ?? Data())
    queue.async {
      self.adverts.append(advert)
      self.register(advert)
    }
  }

  func browse(_ service: String?, handler: MobileDiscoveryHandlerProtocol?) throws {
    guard let handler else { return }
    let search = Search(service: service ?? "", handler: handler)
    queue.async {
      self.searches.append(search)
      self.start(search)
    }
  }

  func stop() throws {
    queue.sync {
      cancel()
      adverts = []
      searches = []
    }
  }

  func restart() {
    queue.async {
      self.cancel()
      self.adverts.forEach(self.register)
      for search in self.searches {
        search.results.restart()
        self.start(search)
      }
    }
  }

  private func cancel() {
    registrations.forEach(DNSServiceRefDeallocate)
    registrations = []
    for search in searches {
      search.browser?.cancel()
      search.browser = nil
      search.lookups.values.forEach { $0.cancel() }
      search.lookups = [:]
    }
  }

  private func register(_ advert: Advert) {
    var ref: DNSServiceRef?
    let status = advert.txt.withUnsafeBytes { txt in
      DNSServiceRegister(&ref, 0, 0, advert.name, advert.service, nil, nil, UInt16(advert.port).bigEndian, UInt16(txt.count), txt.baseAddress, { _, _, error, _, type, _, _ in
        if error != noError { report("advertise \(type.map { String(cString: $0) } ?? "") failed, error \(error)") }
      }, nil)
    }
    guard status == noError, let ref else { return report("advertise \(advert.service) failed, error \(status)") }
    DNSServiceSetDispatchQueue(ref, queue)
    registrations.append(ref)
  }

  private func start(_ search: Search) {
    let browser = NWBrowser(for: .bonjourWithTXTRecord(type: search.service, domain: nil), using: NWParameters())
    browser.stateUpdateHandler = { [weak browser] state in
      guard let browser, search.browser === browser else { return }
      let denied = localNetworkDenied(state)
      if let denied { search.handler.denied(denied) }
      switch state {
      case .waiting(let error), .failed(let error):
        if denied == nil { report("browse \(search.service): \(error)") }
      default:
        break
      }
    }
    browser.browseResultsChangedHandler = { [weak self, weak browser] results, changes in
      guard let self, let browser, search.browser === browser else { return }
      let changed = changes.compactMap { change -> NWEndpoint? in
        if case let .changed(_, new, flags) = change, flags.contains(.metadataChanged) { return new.endpoint }
        return nil
      }
      let (resolve, lost) = search.results.update(results.map(\.endpoint), changed: changed)
      for name in lost {
        search.lookups.removeValue(forKey: name)?.cancel()
        search.handler.lost(name)
      }
      resolve.forEach { self.resolve($0, in: search) }
    }
    search.browser = browser
    browser.start(queue: queue)
  }

  private func resolve(_ endpoint: NWEndpoint, in search: Search) {
    guard let name = serviceName(endpoint) else { return }
    search.lookups.removeValue(forKey: name)?.cancel()
    let lookup = Lookup(queue: queue, what: "resolve \(search.service)") { [weak search] lookup, found in
      guard let search, search.lookups[name] === lookup else { return }
      search.lookups[name] = nil
      if let found { search.handler.found(name, host: found.host, port: found.port, txt: found.txt) }
    }
    search.lookups[name] = lookup
    lookup.start(endpoint)
  }
}
