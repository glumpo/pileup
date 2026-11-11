import AVFoundation
import Photos
import UniformTypeIdentifiers
import XCTest
@testable import PileupNative

private struct Sample {
  let name: String
  let video: Bool
  let display: (Int, Int)
  let redOnLeft: Bool
  let mime: String
  let codec: String?
  var hidden = false
  var file: URL!
  var localID: String!
  var created: Date!
}

private struct Item: Decodable {
  let local_id: String
  let kind: String
  let platform_time: Int64?
  let width: Int
  let height: Int
  let duration_ms: Int64?
  let size_bytes: Int64
  let mime: String
  let codec: String?
  let original_name: String
  let modified: Int64
  let screenshot: Bool
  let hidden: Bool
}

private let base = Date(timeIntervalSince1970: 1_782_986_400)
private let mediaFields: Set<String> = [
  "local_id", "kind", "platform_time", "width", "height", "duration_ms", "size_bytes", "mime", "codec",
  "original_name", "modified", "screenshot", "hidden",
]

private func twoTone(_ width: Int, _ height: Int, topRed: Bool, context: CGContext? = nil) -> CGContext {
  let ctx = context ?? CGContext(
    data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
    space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
  )!
  ctx.setFillColor(red: 1, green: 0, blue: 0, alpha: 1)
  ctx.fill(CGRect(x: 0, y: 0, width: width, height: height))
  ctx.setFillColor(red: 0, green: 0, blue: 1, alpha: 1)
  ctx.fill(topRed
    ? CGRect(x: 0, y: 0, width: width, height: height / 2)
    : CGRect(x: width / 2, y: 0, width: width - width / 2, height: height))
  return ctx
}

private func writeImage(_ url: URL, _ width: Int, _ height: Int, topRed: Bool, type: UTType, orientation: Int? = nil) throws {
  let image = twoTone(width, height, topRed: topRed).makeImage()!
  let destination = try XCTUnwrap(CGImageDestinationCreateWithURL(url as CFURL, type.identifier as CFString, 1, nil))
  var properties: [CFString: Any] = [kCGImageDestinationLossyCompressionQuality: 0.9]
  if let orientation { properties[kCGImagePropertyOrientation] = orientation }
  CGImageDestinationAddImage(destination, image, properties as CFDictionary)
  XCTAssertTrue(CGImageDestinationFinalize(destination), "cannot write \(url.lastPathComponent)")
}

private func writeVideo(_ url: URL, _ width: Int, _ height: Int, topRed: Bool, codec: AVVideoCodecType, transform: CGAffineTransform) async throws {
  let writer = try AVAssetWriter(outputURL: url, fileType: .mov)
  let input = AVAssetWriterInput(mediaType: .video, outputSettings: [
    AVVideoCodecKey: codec, AVVideoWidthKey: width, AVVideoHeightKey: height,
  ])
  input.transform = transform
  let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
    kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32BGRA,
    kCVPixelBufferWidthKey as String: width, kCVPixelBufferHeightKey as String: height,
  ])
  writer.add(input)
  XCTAssertTrue(writer.startWriting(), "\(String(describing: writer.error))")
  writer.startSession(atSourceTime: .zero)
  for frame in 0..<10 {
    while !input.isReadyForMoreMediaData { try await Task.sleep(nanoseconds: 10_000_000) }
    var buffer: CVPixelBuffer?
    CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &buffer)
    let pixels = try XCTUnwrap(buffer)
    CVPixelBufferLockBaseAddress(pixels, [])
    let ctx = CGContext(
      data: CVPixelBufferGetBaseAddress(pixels), width: width, height: height, bitsPerComponent: 8,
      bytesPerRow: CVPixelBufferGetBytesPerRow(pixels), space: CGColorSpace(name: CGColorSpace.sRGB)!,
      bitmapInfo: CGImageAlphaInfo.premultipliedFirst.rawValue | CGBitmapInfo.byteOrder32Little.rawValue
    )!
    _ = twoTone(width, height, topRed: topRed, context: ctx)
    CVPixelBufferUnlockBaseAddress(pixels, [])
    XCTAssertTrue(adaptor.append(pixels, withPresentationTime: CMTime(value: CMTimeValue(frame), timescale: 10)))
  }
  input.markAsFinished()
  writer.endSession(atSourceTime: CMTime(value: 10, timescale: 10))
  await writer.finishWriting()
  XCTAssertEqual(writer.status, .completed, "\(String(describing: writer.error))")
}

private final class Box: @unchecked Sendable {
  var ids: [String] = []
}

private var library: [Sample]?

private func prepareLibrary() async throws -> [Sample] {
  if let library { return library }
  XCTAssertEqual(PHPhotoLibrary.authorizationStatus(for: .readWrite), .authorized, "make ios-test grants photo access to the test host")
  guard PHPhotoLibrary.authorizationStatus(for: .readWrite) == .authorized else { throw mediaError("no photo access") }
  let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
  try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
  var samples = [
    Sample(name: "rotated.jpg", video: false, display: (300, 400), redOnLeft: false, mime: "image/jpeg", codec: nil),
    Sample(name: "wide.png", video: false, display: (500, 200), redOnLeft: true, mime: "image/png", codec: nil),
    Sample(name: "big.heic", video: false, display: (2400, 1800), redOnLeft: true, mime: "image/heic", codec: nil),
    Sample(name: "portrait.mov", video: true, display: (180, 320), redOnLeft: false, mime: "video/quicktime", codec: "avc1"),
    Sample(name: "hevc.mov", video: true, display: (320, 180), redOnLeft: true, mime: "video/quicktime", codec: "hvc1"),
    Sample(name: "hidden.jpg", video: false, display: (64, 48), redOnLeft: true, mime: "image/jpeg", codec: nil, hidden: true),
  ]
  for i in samples.indices {
    samples[i].file = dir.appendingPathComponent(samples[i].name)
    samples[i].created = base.addingTimeInterval(Double(i) * 60 + 0.25)
  }
  try writeImage(samples[0].file, 400, 300, topRed: true, type: .jpeg, orientation: 6)
  try writeImage(samples[1].file, 500, 200, topRed: false, type: .png)
  try writeImage(samples[2].file, 2400, 1800, topRed: false, type: .heic)
  try await writeVideo(samples[3].file, 320, 180, topRed: true, codec: .h264, transform: CGAffineTransform(a: 0, b: 1, c: -1, d: 0, tx: 180, ty: 0))
  try await writeVideo(samples[4].file, 320, 180, topRed: false, codec: .hevc, transform: .identity)
  try writeImage(samples[5].file, 64, 48, topRed: false, type: .jpeg)
  let box = Box()
  let snapshot = samples
  try await PHPhotoLibrary.shared().performChanges {
    for sample in snapshot {
      let request = PHAssetCreationRequest.forAsset()
      request.creationDate = sample.created
      request.isHidden = sample.hidden
      let options = PHAssetResourceCreationOptions()
      options.originalFilename = sample.name
      request.addResource(with: sample.video ? .video : .photo, fileURL: sample.file, options: options)
      box.ids.append(request.placeholderForCreatedAsset!.localIdentifier)
    }
  }
  for i in samples.indices { samples[i].localID = box.ids[i] }
  library = samples
  return samples
}

private func pixels(_ jpeg: Data) throws -> (width: Int, height: Int, red: (Int, Int) -> Bool, blue: (Int, Int) -> Bool) {
  let source = try XCTUnwrap(CGImageSourceCreateWithData(jpeg as CFData, nil))
  XCTAssertEqual(CGImageSourceGetType(source) as String?, UTType.jpeg.identifier)
  let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any]
  XCTAssertEqual(properties?[kCGImagePropertyOrientation] as? Int ?? 1, 1)
  let image = try XCTUnwrap(CGImageSourceCreateImageAtIndex(source, 0, nil))
  let width = image.width, height = image.height
  var raw = [UInt8](repeating: 0, count: width * height * 4)
  raw.withUnsafeMutableBytes { buffer in
    let ctx = CGContext(
      data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
      space: CGColorSpace(name: CGColorSpace.sRGB)!, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
    )!
    ctx.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
  }
  let at = { (x: Int, y: Int) -> (Int, Int) in
    let i = (y * width + x) * 4
    return (Int(raw[i]), Int(raw[i + 2]))
  }
  return (width, height, { let (r, b) = at($0, $1); return r > 180 && b < 90 }, { let (r, b) = at($0, $1); return b > 180 && r < 90 })
}

final class MediaLibraryTests: XCTestCase {
  private let media = MediaLibrary()
  private let renditions = Renditions()

  private func list(_ from: Date, _ to: Date) throws -> [Item] {
    let data = try media.media(milliseconds(from), to: milliseconds(to))
    let raw = try JSONSerialization.jsonObject(with: data) as! [[String: Any]]
    for item in raw { XCTAssertTrue(Set(item.keys).isSubset(of: mediaFields), "\(item.keys)") }
    return try JSONDecoder().decode([Item].self, from: data)
  }

  func testPermissionIsGrantedWithoutPrompt() async throws {
    _ = try await prepareLibrary()
    let granted = await MediaLibrary.requestPermissions()
    XCTAssertTrue(granted)
  }

  func testListDescribesAddedFiles() async throws {
    let samples = try await prepareLibrary()
    let items = try list(base.addingTimeInterval(-3600), base.addingTimeInterval(3600))
    for sample in samples {
      let item = try XCTUnwrap(items.first { $0.local_id == sample.localID }, sample.name)
      XCTAssertEqual(item.kind, sample.video ? "video" : "photo", sample.name)
      XCTAssertEqual(item.platform_time, milliseconds(sample.created), sample.name)
      XCTAssertEqual(item.width, sample.display.0, sample.name)
      XCTAssertEqual(item.height, sample.display.1, sample.name)
      XCTAssertEqual(item.mime, sample.mime, sample.name)
      XCTAssertEqual(item.codec, sample.codec, sample.name)
      XCTAssertEqual(item.original_name, sample.name)
      let size = try FileManager.default.attributesOfItem(atPath: sample.file.path)[.size] as! Int64
      XCTAssertEqual(item.size_bytes, size, sample.name)
      XCTAssertGreaterThan(item.modified, 0, sample.name)
      XCTAssertFalse(item.screenshot, sample.name)
      XCTAssertEqual(item.hidden, sample.hidden, sample.name)
      if sample.video {
        let duration = try XCTUnwrap(item.duration_ms, sample.name)
        XCTAssertEqual(Double(duration), 1000, accuracy: 100, sample.name)
      } else {
        XCTAssertNil(item.duration_ms, sample.name)
      }
    }
  }

  func testRangeHintLeavesOutFarFiles() async throws {
    let samples = try await prepareLibrary()
    let items = try list(base.addingTimeInterval(86_400 * 30), base.addingTimeInterval(86_400 * 31))
    XCTAssertTrue(Set(items.map(\.local_id)).isDisjoint(with: samples.map(\.localID)))
  }

  func testRenditionsAreOrientedJPEGsOfTierSize() async throws {
    let samples = try await prepareLibrary()
    for sample in samples {
      for tier in [1, 2] {
        let expected = fit(sample.display.0, sample.display.1, tier == 1 ? 256 : 1920)
        let image = try pixels(try renditions.rendition(sample.localID, tier: tier))
        let label = "\(sample.name) T\(tier)"
        XCTAssertEqual(image.width, expected.0, label)
        XCTAssertEqual(image.height, expected.1, label)
        let left = image.width / 4, right = image.width * 3 / 4, middle = image.height / 2
        XCTAssertTrue(sample.redOnLeft ? image.red(left, middle) : image.blue(left, middle), label)
        XCTAssertTrue(sample.redOnLeft ? image.blue(right, middle) : image.red(right, middle), label)
      }
    }
  }

  func testUnknownTierIsRejected() async throws {
    let samples = try await prepareLibrary()
    XCTAssertThrowsError(try renditions.rendition(samples[0].localID, tier: 3))
  }

  func testOriginalIsReadByRangesAndMatchesFile() async throws {
    let samples = try await prepareLibrary()
    for sample in samples {
      let file = try Data(contentsOf: sample.file)
      var size: Int64 = 0
      let started = Date()
      try renditions.originalSize(sample.localID, ret0_: &size)
      XCTAssertLessThan(Date().timeIntervalSince(started), 5, sample.name)
      XCTAssertEqual(size, Int64(file.count), sample.name)
      var read = Data()
      while true {
        let chunk = try renditions.readOriginal(sample.localID, offset: Int64(read.count), length: 4096)
        if chunk.isEmpty { break }
        XCTAssertLessThanOrEqual(chunk.count, 4096)
        read.append(chunk)
      }
      XCTAssertEqual(read, file, sample.name)
      XCTAssertEqual(try renditions.readOriginal(sample.localID, offset: 7, length: 100), file.subdata(in: 7..<107), sample.name)
      XCTAssertEqual(try renditions.readOriginal(sample.localID, offset: size + 1, length: 100), Data(), sample.name)
    }
  }

  func testUnknownFileFailsFastAndIsNotCalledCloudOnly() throws {
    let started = Date()
    var size: Int64 = 0
    XCTAssertThrowsError(try renditions.originalSize("00000000-0000-0000-0000-000000000000/L0/001", ret0_: &size)) { error in
      XCTAssertFalse(error.localizedDescription.hasPrefix("not_local:"))
    }
    XCTAssertThrowsError(try renditions.rendition("00000000-0000-0000-0000-000000000000/L0/001", tier: 1))
    XCTAssertLessThan(Date().timeIntervalSince(started), 1)
  }

  func testCoreSelectsLibraryFilesAndRendersThem() async throws {
    let samples = try await prepareLibrary()
    let keystore = KeychainKeystore(service: "test.\(UUID().uuidString)")
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try Core.start(dir: dir.appendingPathComponent("core"), keystore: keystore, appVersion: "1")
    defer {
      try? Core.stop()
      try? keystore.reset()
      try? FileManager.default.removeItem(at: dir)
    }
    func call(_ path: String, method: String = "GET", body: String? = nil) async throws -> (Int, Data, String?) {
      var request = URLRequest(url: URL(string: "http://127.0.0.1:\(Core.port)\(path)")!)
      request.httpMethod = method
      request.setValue("Bearer \(Core.token)", forHTTPHeaderField: "Authorization")
      request.httpBody = body.map { Data($0.utf8) }
      request.setValue("application/json", forHTTPHeaderField: "Content-Type")
      let (data, response) = try await URLSession.shared.data(for: request)
      let http = response as! HTTPURLResponse
      return (http.statusCode, data, http.value(forHTTPHeaderField: "Content-Type"))
    }
    struct Pile: Decodable { let id: String }
    struct Asset: Decodable { let id: String; let local_id: String; let kind: String; let selected: Bool }
    struct Selection: Decodable { let assets: [Asset] }
    struct Entry: Decodable { let id: String; let revision: String; let thumbhash: String }
    let (_, created, _) = try await call("/piles", method: "POST", body: #"{"name":"Trip","member":"iPhone"}"#)
    let pile = try JSONDecoder().decode(Pile.self, from: created).id
    let from = milliseconds(base.addingTimeInterval(-3600)), to = milliseconds(base.addingTimeInterval(3600))
    let (selected, _, _) = try await call("/piles/\(pile)/selection", method: "PUT", body: #"{"from":\#(from),"to":\#(to)}"#)
    XCTAssertEqual(selected, 204)
    let (_, body, _) = try await call("/piles/\(pile)/selection")
    let assets = try JSONDecoder().decode(Selection.self, from: body).assets
    for sample in samples {
      let asset = try XCTUnwrap(assets.first { $0.local_id == sample.localID }, sample.name)
      XCTAssertEqual(asset.kind, sample.video ? "video" : "photo", sample.name)
      XCTAssertEqual(asset.selected, !sample.hidden, sample.name)
    }
    let shown = Set(samples.filter { !$0.hidden }.compactMap { s in assets.first { $0.local_id == s.localID }?.id })
    var entries: [Entry] = []
    for _ in 0..<60 {
      let (_, timeline, _) = try await call("/piles/\(pile)/timeline")
      entries = try JSONDecoder().decode([Entry].self, from: timeline).filter { shown.contains($0.id) }
      if entries.count == shown.count, entries.allSatisfy({ !$0.thumbhash.isEmpty }) { break }
      try await Task.sleep(nanoseconds: 500_000_000)
    }
    XCTAssertEqual(entries.count, shown.count)
    XCTAssertTrue(entries.allSatisfy { !$0.thumbhash.isEmpty }, "thumbhash comes from T1 rendered by the core")
    let heic = try XCTUnwrap(assets.first { $0.local_id == samples[2].localID })
    let entry = try XCTUnwrap(entries.first { $0.id == heic.id })
    let (status, t2, type) = try await call("/piles/\(pile)/blob/\(heic.id)/\(entry.revision)/2")
    XCTAssertEqual(status, 200)
    XCTAssertEqual(type, "image/jpeg")
    let image = try pixels(t2)
    XCTAssertEqual(image.width, 1920)
    XCTAssertEqual(image.height, 1440)
  }
}
