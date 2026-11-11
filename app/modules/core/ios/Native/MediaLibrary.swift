import AVFoundation
import Foundation
import Mobile
import Photos

final class MediaLibrary: NSObject, MobileMediaProviderProtocol {
  private let lock = NSLock()
  private var codecs: [String: (Date?, String)] = [:]

  static func requestPermissions() async -> Bool {
    mediaGranted(await PHPhotoLibrary.requestAuthorization(for: .readWrite))
  }

  func media(_ from: Int64, to: Int64) throws -> Data {
    guard mediaGranted(PHPhotoLibrary.authorizationStatus(for: .readWrite)) else {
      MobileReport("media", "no access to the photo library, the list is empty")
      return mediaJSON([])
    }
    let (start, end) = queryInterval(from: from, to: to)
    let options = PHFetchOptions()
    options.includeHiddenAssets = true
    options.predicate = NSPredicate(
      format: "(mediaType == %d OR mediaType == %d) AND creationDate >= %@ AND creationDate <= %@",
      PHAssetMediaType.image.rawValue, PHAssetMediaType.video.rawValue, start as NSDate, end as NSDate
    )
    let fetched = PHAsset.fetchAssets(with: options)
    let assets = fetched.objects(at: IndexSet(integersIn: 0..<fetched.count))
    var records = assets.map(record)
    let videos = records.indices.filter { records[$0].video }
    let found = Codecs(videos.count)
    DispatchQueue.concurrentPerform(iterations: videos.count) { i in
      found.set(i, self.codec(assets[videos[i]]))
    }
    for (i, index) in videos.enumerated() {
      records[index].codec = found.values[i]
    }
    return mediaJSON(records)
  }

  private func record(_ asset: PHAsset) -> MediaRecord {
    let video = asset.mediaType == .video
    let resources = PHAssetResource.assetResources(for: asset)
    let served = servedResource(resources.map(\.type), video: video).map { resources[$0] }
    let primary = resources.first { $0.type == (video ? PHAssetResourceType.video : .photo) } ?? served
    return MediaRecord(
      localID: asset.localIdentifier,
      video: video,
      created: asset.creationDate,
      modified: asset.modificationDate,
      width: asset.pixelWidth,
      height: asset.pixelHeight,
      duration: asset.duration,
      size: served.map(fileSize) ?? 0,
      mime: served.map { mimeType(uti: $0.uniformTypeIdentifier) } ?? "",
      name: servedName(original: primary?.originalFilename ?? "", served: served?.originalFilename ?? ""),
      codec: "",
      screenshot: asset.mediaSubtypes.contains(.photoScreenshot),
      hidden: asset.isHidden
    )
  }

  private func codec(_ asset: PHAsset) -> String {
    lock.lock()
    let cached = codecs[asset.localIdentifier]
    lock.unlock()
    if let cached, cached.0 == asset.modificationDate { return cached.1 }
    var codec = ""
    do {
      if let format = try videoAsset(asset)?.tracks(withMediaType: .video).first?.formatDescriptions.first {
        codec = fourCC(CMFormatDescriptionGetMediaSubType(format as! CMFormatDescription))
      }
    } catch {
      if !inCloud(error) { MobileReport("media", "codec of \(asset.localIdentifier): \(error.localizedDescription)") }
    }
    lock.lock()
    codecs[asset.localIdentifier] = (asset.modificationDate, codec)
    lock.unlock()
    return codec
  }
}

private final class Codecs: @unchecked Sendable {
  private let lock = NSLock()
  private(set) var values: [String]

  init(_ count: Int) {
    values = Array(repeating: "", count: count)
  }

  func set(_ i: Int, _ codec: String) {
    lock.lock()
    values[i] = codec
    lock.unlock()
  }
}

func fileSize(_ resource: PHAssetResource) -> Int64 {
  guard resource.responds(to: Selector(("fileSize"))) else { return 0 }
  return (resource.value(forKey: "fileSize") as? NSNumber)?.int64Value ?? 0
}

func fetchAsset(_ localID: String) throws -> PHAsset {
  let options = PHFetchOptions()
  options.includeHiddenAssets = true
  guard let asset = PHAsset.fetchAssets(withLocalIdentifiers: [localID], options: options).firstObject else {
    throw mediaError("\(localID) is not in the photo library")
  }
  return asset
}

let photoKitTimeout: TimeInterval = 15

func videoAsset(_ asset: PHAsset) throws -> AVAsset? {
  let options = PHVideoRequestOptions()
  options.version = .current
  options.deliveryMode = .highQualityFormat
  options.isNetworkAccessAllowed = false
  var result: AVAsset?
  var details: [AnyHashable: Any]?
  let done = DispatchSemaphore(value: 0)
  PHImageManager.default().requestAVAsset(forVideo: asset, options: options) { video, _, info in
    result = video
    details = info
    done.signal()
  }
  guard done.wait(timeout: .now() + photoKitTimeout) == .success else {
    throw mediaError("photo library did not answer for \(asset.localIdentifier) in \(Int(photoKitTimeout)) s")
  }
  if result == nil, inCloud(details) { throw notLocal(asset.localIdentifier) }
  if result == nil, let error = details?[PHImageErrorKey] as? Error { throw error }
  return result
}
