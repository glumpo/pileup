import Photos
import XCTest
@testable import PileupNative

final class MediaTests: XCTestCase {
  private let photo = MediaRecord(
    localID: "1B2C3D4E-0000-4000-8000-00000000A001/L0/001",
    video: false,
    created: Date(timeIntervalSince1970: 1_782_986_400.123),
    modified: Date(timeIntervalSince1970: 1_782_986_600),
    width: 3024,
    height: 4032,
    duration: 0,
    size: 2_500_000,
    mime: "image/heic",
    name: "IMG_0001.HEIC",
    codec: "",
    screenshot: false,
    hidden: false
  )

  private func items(_ records: [MediaRecord]) throws -> [NSDictionary] {
    try JSONSerialization.jsonObject(with: mediaJSON(records)) as! [NSDictionary]
  }

  func testPhotoMatchesCoreMediaItem() throws {
    XCTAssertEqual(try items([photo]), [[
      "local_id": "1B2C3D4E-0000-4000-8000-00000000A001/L0/001",
      "kind": "photo",
      "platform_time": 1_782_986_400_123,
      "width": 3024,
      "height": 4032,
      "size_bytes": 2_500_000,
      "mime": "image/heic",
      "original_name": "IMG_0001.HEIC",
      "modified": 1_782_986_600_000,
      "screenshot": false,
      "hidden": false,
    ] as NSDictionary])
  }

  func testVideoCarriesDurationAndCodec() throws {
    var video = photo
    video.video = true
    video.duration = 15.2504
    video.mime = "video/quicktime"
    video.codec = "hvc1"
    let item = try items([video])[0]
    XCTAssertEqual(item["kind"] as? String, "video")
    XCTAssertEqual(item["duration_ms"] as? Int, 15250)
    XCTAssertEqual(item["codec"] as? String, "hvc1")
    XCTAssertEqual(item["mime"] as? String, "video/quicktime")
  }

  func testMissingDatesAreAbsentOrZero() throws {
    var undated = photo
    undated.created = nil
    undated.modified = nil
    let item = try items([undated])[0]
    XCTAssertNil(item["platform_time"])
    XCTAssertEqual(item["modified"] as? Int, 0)
  }

  func testFlagsAreReported() throws {
    var flagged = photo
    flagged.screenshot = true
    flagged.hidden = true
    let item = try items([flagged])[0]
    XCTAssertEqual(item["screenshot"] as? Bool, true)
    XCTAssertEqual(item["hidden"] as? Bool, true)
  }

  func testNamesSurviveEscaping() throws {
    var odd = photo
    odd.name = "a\"b\\c\nd\u{1}é.jpg"
    XCTAssertEqual(try items([odd])[0]["original_name"] as? String, "a\"b\\c\nd\u{1}é.jpg")
  }

  func testEmptyLibraryIsEmptyArray() {
    XCTAssertEqual(String(decoding: mediaJSON([]), as: UTF8.self), "[]")
  }

  func testQueryWidensRangeByMargin() {
    let from: Int64 = 1_782_986_400_000
    let to = from + 86_400_000
    let (start, end) = queryInterval(from: from, to: to)
    XCTAssertEqual(start.timeIntervalSince1970, 1_782_986_400 - 14 * 3600)
    XCTAssertEqual(end.timeIntervalSince1970, 1_782_986_400 + 86_400 + 14 * 3600)
  }

  func testQueryDoesNotOverflowOnOpenRange() {
    let (start, end) = queryInterval(from: .min, to: .max)
    XCTAssertLessThanOrEqual(start, Date(timeIntervalSince1970: -1e10))
    XCTAssertGreaterThanOrEqual(end, Date(timeIntervalSince1970: 1e10))
  }

  func testCurrentVersionOfOriginalIsServed() {
    XCTAssertEqual(servedResource([.photo, .adjustmentData, .fullSizePhoto], video: false), 2)
    XCTAssertEqual(servedResource([.photo, .pairedVideo], video: false), 0)
    XCTAssertEqual(servedResource([.video, .fullSizeVideo], video: true), 1)
    XCTAssertEqual(servedResource([.adjustmentData, .video], video: true), 1)
    XCTAssertNil(servedResource([.pairedVideo], video: false))
    XCTAssertNil(servedResource([.photo], video: true))
  }

  func testServedNameKeepsOriginalNameWithServedExtension() {
    XCTAssertEqual(servedName(original: "IMG_0001.HEIC", served: "IMG_0001.HEIC"), "IMG_0001.HEIC")
    XCTAssertEqual(servedName(original: "IMG_0001.HEIC", served: "FullSizeRender.heic"), "IMG_0001.HEIC")
    XCTAssertEqual(servedName(original: "IMG_0001.HEIC", served: "FullSizeRender.jpg"), "IMG_0001.jpg")
    XCTAssertEqual(servedName(original: "IMG_0002.MOV", served: "FullSizeRender.mov"), "IMG_0002.MOV")
    XCTAssertEqual(servedName(original: "IMG_0003", served: ""), "IMG_0003")
  }

  func testMimeComesFromTypeIdentifier() {
    XCTAssertEqual(mimeType(uti: "public.heic"), "image/heic")
    XCTAssertEqual(mimeType(uti: "public.jpeg"), "image/jpeg")
    XCTAssertEqual(mimeType(uti: "public.png"), "image/png")
    XCTAssertEqual(mimeType(uti: "com.apple.quicktime-movie"), "video/quicktime")
    XCTAssertEqual(mimeType(uti: "public.mpeg-4"), "video/mp4")
    XCTAssertEqual(mimeType(uti: "not.a.type"), "")
  }

  func testCodecIsFourCharacterCode() {
    XCTAssertEqual(fourCC(0x6876_6331), "hvc1")
    XCTAssertEqual(fourCC(0x6176_6331), "avc1")
  }

  func testLimitedAccessCountsAsAccess() {
    XCTAssertTrue(mediaGranted(.authorized))
    XCTAssertTrue(mediaGranted(.limited))
    XCTAssertFalse(mediaGranted(.denied))
    XCTAssertFalse(mediaGranted(.restricted))
    XCTAssertFalse(mediaGranted(.notDetermined))
  }

  func testTiersHaveDocumentedLongSide() throws {
    XCTAssertEqual(try maxSide(1), 256)
    XCTAssertEqual(try maxSide(2), 1920)
    XCTAssertThrowsError(try maxSide(0))
    XCTAssertThrowsError(try maxSide(3))
  }

  func testFitLimitsLongSideKeepingAspect() {
    XCTAssert(fit(4000, 3000, 256) == (256, 192))
    XCTAssert(fit(3000, 4000, 256) == (192, 256))
    XCTAssert(fit(7680, 4320, 3840) == (3840, 2160))
    XCTAssert(fit(10000, 10, 256) == (256, 1))
    XCTAssert(fit(200, 100, 256) == (200, 100))
    XCTAssert(fit(256, 256, 256) == (256, 256))
  }

  func testRenditionKeepsAssetAspectAndDoesNotUpscaleDerivative() {
    XCTAssert(renditionSize(asset: (4032, 3024), image: (1920, 1440), side: 1920) == (1920, 1440))
    XCTAssert(renditionSize(asset: (4032, 3024), image: (255, 192), side: 256) == (255, 191))
    XCTAssert(renditionSize(asset: (4032, 3024), image: (480, 360), side: 1920) == (480, 360))
    XCTAssert(renditionSize(asset: (300, 400), image: (300, 400), side: 1920) == (300, 400))
    XCTAssert(renditionSize(asset: (0, 0), image: (640, 480), side: 256) == (256, 192))
  }

  func testReadLengthStaysInsideFile() {
    XCTAssertEqual(readLength(0, 1024, 10_000), 1024)
    XCTAssertEqual(readLength(9_984, 1024, 10_000), 16)
    XCTAssertEqual(readLength(10_000, 1024, 10_000), 0)
    XCTAssertEqual(readLength(20_000, 1024, 10_000), 0)
    XCTAssertEqual(readLength(0, .max, .max), Int(Int32.max))
  }

  func testNotLocalErrorReachesGoWithPrefix() {
    let error = notLocal("ABC/L0/001")
    XCTAssertTrue(error.localizedDescription.hasPrefix("not_local: "))
    XCTAssertTrue(error.localizedDescription.contains("ABC/L0/001"))
  }

  func testCloudOnlyIsRecognised() {
    let network = NSError(domain: PHPhotosErrorDomain, code: PHPhotosError.networkAccessRequired.rawValue)
    XCTAssertTrue(inCloud(network))
    XCTAssertFalse(inCloud(NSError(domain: PHPhotosErrorDomain, code: PHPhotosError.invalidResource.rawValue)))
    XCTAssertFalse(inCloud(nil as Error?))
    XCTAssertTrue(inCloud([PHImageResultIsInCloudKey: true]))
    XCTAssertTrue(inCloud([PHImageErrorKey: network]))
    XCTAssertFalse(inCloud([PHImageResultIsInCloudKey: false]))
    XCTAssertFalse(inCloud(nil as [AnyHashable: Any]?))
  }
}
