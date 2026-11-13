import Foundation
import Photos
import UniformTypeIdentifiers

let queryMargin: TimeInterval = 14 * 60 * 60
let jpegQuality: CGFloat = 0.85

struct MediaRecord {
  var localID: String
  var video: Bool
  var created: Date?
  var modified: Date?
  var width: Int
  var height: Int
  var duration: TimeInterval
  var size: Int64
  var mime: String
  var name: String
  var codec: String
  var screenshot: Bool
  var hidden: Bool
}

private struct MediaItem: Encodable {
  let localId: String
  let kind: String
  let platformTime: Int64?
  let width: Int
  let height: Int
  let durationMs: Int64?
  let sizeBytes: Int64
  let mime: String
  let codec: String?
  let originalName: String
  let modified: Int64
  let screenshot: Bool
  let hidden: Bool
}

func milliseconds(_ date: Date) -> Int64 {
  Int64((date.timeIntervalSince1970 * 1000).rounded())
}

func mediaJSON(_ records: [MediaRecord]) -> Data {
  let items = records.map { r in
    MediaItem(
      localId: r.localID,
      kind: r.video ? "video" : "photo",
      platformTime: r.created.map(milliseconds),
      width: r.width,
      height: r.height,
      durationMs: r.video ? Int64((r.duration * 1000).rounded()) : nil,
      sizeBytes: r.size,
      mime: r.mime,
      codec: r.codec.isEmpty ? nil : r.codec,
      originalName: r.name,
      modified: r.modified.map(milliseconds) ?? 0,
      screenshot: r.screenshot,
      hidden: r.hidden
    )
  }
  let encoder = JSONEncoder()
  encoder.keyEncodingStrategy = .convertToSnakeCase
  return try! encoder.encode(items)
}

func queryInterval(from: Int64, to: Int64) -> (Date, Date) {
  let start = max(Double(from) / 1000 - queryMargin, Date.distantPast.timeIntervalSince1970)
  let end = min(Double(to) / 1000 + queryMargin, Date.distantFuture.timeIntervalSince1970)
  return (Date(timeIntervalSince1970: start), Date(timeIntervalSince1970: end))
}

func servedResource(_ types: [PHAssetResourceType], video: Bool) -> Int? {
  let preferred: [PHAssetResourceType] = video ? [.fullSizeVideo, .video] : [.fullSizePhoto, .photo]
  return preferred.lazy.compactMap { types.firstIndex(of: $0) }.first
}

func servedName(original: String, served: String) -> String {
  let ext = (served as NSString).pathExtension
  guard !ext.isEmpty, ext.lowercased() != (original as NSString).pathExtension.lowercased() else { return original }
  return ((original as NSString).deletingPathExtension as NSString).appendingPathExtension(ext) ?? original
}

func mimeType(uti: String) -> String {
  UTType(uti)?.preferredMIMEType ?? ""
}

func fourCC(_ code: FourCharCode) -> String {
  String(decoding: [24, 16, 8, 0].map { UInt8((code >> $0) & 0xff) }, as: UTF8.self)
}

func mediaGranted(_ status: PHAuthorizationStatus) -> Bool {
  status == .authorized || status == .limited
}

func maxSide(_ tier: Int) throws -> Int {
  switch tier {
  case 1: return 256
  case 2: return 1920
  default: throw mediaError("no rendition for tier \(tier)")
  }
}

func fit(_ width: Int, _ height: Int, _ side: Int) -> (Int, Int) {
  let long = max(width, height)
  guard long > side else { return (width, height) }
  let scale = Double(side) / Double(long)
  return (max(1, Int((Double(width) * scale).rounded())), max(1, Int((Double(height) * scale).rounded())))
}

func renditionSize(asset: (Int, Int), image: (Int, Int), side: Int) -> (Int, Int) {
  let (width, height) = asset.0 > 0 && asset.1 > 0 ? asset : image
  return fit(width, height, min(side, max(image.0, image.1)))
}

func readLength(_ offset: Int64, _ length: Int64, _ size: Int64) -> Int {
  Int(max(0, min(length, size - offset, Int64(Int32.max))))
}

func inCloud(_ error: Error?) -> Bool {
  guard let error = error as NSError? else { return false }
  return error.domain == PHPhotosErrorDomain && error.code == PHPhotosError.networkAccessRequired.rawValue
}

func inCloud(_ info: [AnyHashable: Any]?) -> Bool {
  info?[PHImageResultIsInCloudKey] as? Bool == true || inCloud(info?[PHImageErrorKey] as? Error)
}

func notLocal(_ localID: String) -> NSError {
  mediaError("not_local: \(localID) is only in iCloud, not on this iPhone")
}

func mediaError(_ message: String) -> NSError {
  NSError(domain: "pileup.media", code: 1, userInfo: [NSLocalizedDescriptionKey: message])
}
