import AVFoundation
import Mobile
import Network
import UIKit
import XCTest
@testable import PileupNative

final class SlideTests: XCTestCase {
  func testSlideIsReadFromWhatTheAppPresents() throws {
    let slide = try XCTUnwrap(Slide([
      "asset": "a1",
      "image": "http://192.168.1.5:50123/show/t/a1/r1/2",
      "video": "http://192.168.1.5:50123/show/t/a1/r1/3",
      "around": ["http://192.168.1.5:50123/show/t/c9/r9/2", "http://192.168.1.5:50123/show/t/f3/r3/2"],
      "playing": true,
    ]))
    XCTAssertEqual(slide.asset, "a1")
    XCTAssertEqual(slide.image.absoluteString, "http://192.168.1.5:50123/show/t/a1/r1/2")
    XCTAssertEqual(slide.video?.absoluteString, "http://192.168.1.5:50123/show/t/a1/r1/3")
    XCTAssertTrue(slide.playing)
    XCTAssertEqual(slide.images.count, 3)

    let photo = try XCTUnwrap(Slide(["asset": "f3", "image": "http://192.168.1.5:50123/show/t/f3/r3/2", "around": []]))
    XCTAssertNil(photo.video)
    XCTAssertFalse(photo.playing)
    XCTAssertEqual(photo.images, [photo.image])

    XCTAssertNil(Slide(nil))
    XCTAssertNil(Slide(["asset": "f3", "around": []]))
  }

  func testOnlyPlayingAndBufferingAreReportedFromThePlayer() {
    XCTAssertEqual(playerState(.playing), "playing")
    XCTAssertEqual(playerState(.waitingToPlayAtSpecifiedRate), "buffering")
    XCTAssertNil(playerState(.paused))
  }

  func testPlayerErrorsTellNetworkFromFormat() {
    func reason(_ domain: String, _ code: Int, under: NSError? = nil) -> String? {
      playbackFailure(NSError(domain: domain, code: code, userInfo: under.map { [NSUnderlyingErrorKey: $0] } ?? [:])).reason
    }
    XCTAssertEqual(reason(NSURLErrorDomain, NSURLErrorCannotConnectToHost), "network")
    XCTAssertEqual(reason(NSURLErrorDomain, NSURLErrorTimedOut), "network")
    XCTAssertEqual(reason(AVFoundationErrorDomain, AVError.Code.unknown.rawValue, under: NSError(domain: NSURLErrorDomain, code: NSURLErrorNetworkConnectionLost)), "network")
    XCTAssertEqual(reason(AVFoundationErrorDomain, AVError.Code.serverIncorrectlyConfigured.rawValue), "network")
    XCTAssertEqual(reason(AVFoundationErrorDomain, AVError.Code.decodeFailed.rawValue), "unsupported")
    XCTAssertEqual(reason(AVFoundationErrorDomain, AVError.Code.fileFormatNotRecognized.rawValue), "unsupported")
    XCTAssertEqual(reason(AVFoundationErrorDomain, AVError.Code.decoderNotFound.rawValue), "unsupported")
    XCTAssertNil(reason(AVFoundationErrorDomain, AVError.Code.unknown.rawValue))
    XCTAssertNil(reason(NSOSStatusErrorDomain, -12938))
    XCTAssertNil(playbackFailure(nil).reason)

    let failure = playbackFailure(NSError(domain: AVFoundationErrorDomain, code: -11800, userInfo: [
      NSUnderlyingErrorKey: NSError(domain: NSOSStatusErrorDomain, code: -12938),
      NSLocalizedDescriptionKey: "The operation could not be completed",
    ]))
    XCTAssertEqual(failure.message, "AVFoundationErrorDomain -11800 / NSOSStatusErrorDomain -12938: The operation could not be completed")
  }
}

private func silentServer() -> (fd: Int32, port: Int) {
  let fd = socket(AF_INET, SOCK_STREAM, 0)
  var address = sockaddr_in()
  address.sin_family = sa_family_t(AF_INET)
  address.sin_addr.s_addr = inet_addr("127.0.0.1")
  var length = socklen_t(MemoryLayout<sockaddr_in>.size)
  withUnsafeMutablePointer(to: &address) {
    $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
      _ = bind(fd, $0, length)
      _ = getsockname(fd, $0, &length)
    }
  }
  listen(fd, 8)
  return (fd, Int(UInt16(bigEndian: address.sin_port)))
}

final class PlayerTests: XCTestCase {
  func testVideoFollowsTheCoreAfterReturnAndInterruption() throws {
    let server = silentServer()
    defer { close(server.fd) }
    let base = "http://127.0.0.1:\(server.port)"
    let presentation = Presentation()
    let screen = PresentationController()
    screen.loadViewIfNeeded()
    presentation.attach(screen)
    let playing: [String: Any] = ["asset": "v1", "image": "\(base)/v1/2", "video": "\(base)/v1/3", "around": [], "playing": true]
    presentation.show(Slide(playing))
    XCTAssertTrue(eventually(2) { presentation.player.timeControlStatus != .paused })

    presentation.player.pause()
    NotificationCenter.default.post(name: UIApplication.didBecomeActiveNotification, object: nil)
    XCTAssertTrue(eventually(2) { presentation.player.timeControlStatus != .paused }, "video paused by the system stays paused after return")

    presentation.player.pause()
    NotificationCenter.default.post(name: AVAudioSession.interruptionNotification, object: AVAudioSession.sharedInstance(), userInfo: [
      AVAudioSessionInterruptionTypeKey: AVAudioSession.InterruptionType.ended.rawValue,
    ])
    XCTAssertTrue(eventually(2) { presentation.player.timeControlStatus != .paused }, "video paused by an interruption stays paused after it ended")

    presentation.show(Slide(playing.merging(["playing": false]) { _, b in b }))
    XCTAssertEqual(presentation.player.timeControlStatus, .paused)
    NotificationCenter.default.post(name: UIApplication.didBecomeActiveNotification, object: nil)
    XCTAssertFalse(eventually(1) { presentation.player.timeControlStatus != .paused }, "video paused on the remote started after return")

    presentation.show(nil)
    XCTAssertNil(presentation.player.currentItem)
    presentation.detach(screen)
  }
}

private final class Album: NSObject, MobileMediaProviderProtocol, MobileRenditionMakerProtocol {
  static let taken: Int64 = 1_783_000_000_000
  let jpeg: Data = {
    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    return UIGraphicsImageRenderer(size: CGSize(width: 64, height: 48), format: format).jpegData(withCompressionQuality: 0.9) { context in
      UIColor.red.setFill()
      context.fill(CGRect(x: 0, y: 0, width: 64, height: 48))
    }
  }()

  func media(_ from: Int64, to: Int64) throws -> Data {
    Data(#"[{"local_id":"p1","kind":"photo","platform_time":\#(Album.taken),"width":64,"height":48,"size_bytes":\#(jpeg.count),"mime":"image/jpeg","original_name":"p1.jpg","modified":1}]"#.utf8)
  }

  func rendition(_ localID: String?, tier: Int) throws -> Data { jpeg }

  func originalSize(_ localID: String?, ret0_: UnsafeMutablePointer<Int64>?) throws {
    ret0_?.pointee = Int64(jpeg.count)
  }

  func readOriginal(_ localID: String?, offset: Int64, length: Int64) throws -> Data {
    jpeg.subdata(in: Int(offset)..<min(jpeg.count, Int(offset + length)))
  }
}

final class PresentationTests: XCTestCase {
  private let keystore = KeychainKeystore(service: "test.\(UUID().uuidString)")
  private let television = Discovery()
  private var dir: URL!

  override func setUpWithError() throws {
    dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("core")
    try prepareDataDirectory(dir, keystore: keystore)
    let album = Album()
    var error: NSError?
    guard MobileStart(dir.path, "ios", "1.2.3", keystore, album, album, NetworkAddresses(), Core.discovery, &error) else { throw error! }
  }

  override func tearDownWithError() throws {
    try television.stop()
    try Core.stop()
    try keystore.reset()
    try? FileManager.default.removeItem(at: dir.deletingLastPathComponent())
  }

  private func call(_ method: String, _ path: String, _ body: String? = nil) -> (Int, Any?) {
    var request = URLRequest(url: URL(string: "http://127.0.0.1:\(Core.port)\(path)")!)
    request.httpMethod = method
    request.setValue("Bearer \(Core.token)", forHTTPHeaderField: "Authorization")
    if let body {
      request.httpBody = Data(body.utf8)
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
    }
    var result: (Int, Any?) = (0, nil)
    let done = DispatchSemaphore(value: 0)
    URLSession.shared.dataTask(with: request) { data, response, _ in
      result = ((response as? HTTPURLResponse)?.statusCode ?? 0, data.flatMap { try? JSONSerialization.jsonObject(with: $0) })
      done.signal()
    }.resume()
    done.wait()
    return result
  }

  private func appleTV(_ name: String) throws -> String {
    let entry = "model=AppleTV14,1"
    try television.advertise("_airplay._tcp", name: name, port: 7000, txt: Data([UInt8(entry.utf8.count)]) + Data(entry.utf8))
    var id: String?
    XCTAssertTrue(eventually(30) {
      let screens = call("GET", "/screens").1 as? [[String: Any]] ?? []
      id = screens.first { $0["name"] as? String == name && $0["available"] as? Bool == true }?["id"] as? String
      return id != nil
    }, "Apple TV is not listed")
    return try XCTUnwrap(id)
  }

  func testPresentationShowsTheSlideTheCoreServesOnTheLANAddress() throws {
    let (created, body) = call("POST", "/piles", #"{"name":"Trip","member":"Phone"}"#)
    XCTAssertEqual(created, 201)
    let pile = try XCTUnwrap((body as? [String: Any])?["id"] as? String)
    let (selected, _) = call("PUT", "/piles/\(pile)/selection", #"{"from":\#(Album.taken - 3_600_000),"to":\#(Album.taken + 3_600_000)}"#)
    XCTAssertEqual(selected, 204)
    XCTAssertTrue(eventually { (call("GET", "/piles/\(pile)/timeline").1 as? [Any])?.count == 1 }, "photo is not in the timeline")
    let tv = try appleTV("Pileup TV \(UUID().uuidString.prefix(8))")

    let (started, state) = call("POST", "/show", #"{"pile":"\#(pile)","screen":"\#(tv)"}"#)
    XCTAssertEqual(started, 201)
    let show = try XCTUnwrap(state as? [String: Any])
    let slide = try XCTUnwrap(show["slide"] as? [String: Any])
    let asset = try XCTUnwrap((show["current"] as? [String: Any])?["id"] as? String)
    let image = try XCTUnwrap(slide["image"] as? String)
    let host = try XCTUnwrap(URL(string: image)?.host)
    XCTAssertNotNil(IPv4Address(host), host)
    XCTAssertNotEqual(host, "127.0.0.1")

    let presentation = Presentation()
    var displays: [Bool] = []
    presentation.onDisplay = { displays.append($0) }
    let screen = PresentationController()
    screen.loadViewIfNeeded()
    presentation.attach(screen)
    XCTAssertFalse(screen.idleLabel.isHidden)

    presentation.show(Slide(["asset": asset, "image": image, "around": [], "playing": false]))
    XCTAssertTrue(screen.idleLabel.isHidden)
    XCTAssertTrue(eventually { screen.imageView.image != nil }, "slide is not shown")
    XCTAssertEqual(screen.imageView.image?.size, CGSize(width: 64, height: 48))

    let forbidden = image.replacingOccurrences(of: "/show/", with: "/show/wrong")
    presentation.show(Slide(["asset": asset, "image": forbidden, "around": [], "playing": false]))
    XCTAssertTrue(eventually { screen.imageView.image == nil }, "a slide that does not load keeps the previous one")
    XCTAssertTrue(screen.idleLabel.isHidden)

    presentation.show(nil)
    XCTAssertNil(screen.imageView.image)
    XCTAssertFalse(screen.idleLabel.isHidden)
    presentation.detach(screen)
    XCTAssertEqual(displays, [true, false])
  }
}
