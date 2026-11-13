import AVFoundation
import Foundation
import Mobile
import Photos
import UIKit

final class Renditions: NSObject, MobileRenditionMakerProtocol {
  private let originals = Originals(open: openOriginal)

  func rendition(_ localID: String?, tier: Int) throws -> Data {
    let side = try maxSide(tier)
    let id = localID ?? ""
    let asset = try fetchAsset(id)
    let (best, info) = request(asset, side: side, mode: .highQualityFormat)
    guard let image = best ?? request(asset, side: side, mode: .fastFormat).0 else {
      if inCloud(info) { throw notLocal(id) }
      throw (info?[PHImageErrorKey] as? Error) ?? mediaError("photo library gave no image for \(id)") as Error
    }
    let (width, height) = renditionSize(
      asset: (asset.pixelWidth, asset.pixelHeight),
      image: (Int((image.size.width * image.scale).rounded()), Int((image.size.height * image.scale).rounded())),
      side: side
    )
    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    format.opaque = true
    format.preferredRange = .standard
    let rect = CGRect(x: 0, y: 0, width: width, height: height)
    return UIGraphicsImageRenderer(size: rect.size, format: format).jpegData(withCompressionQuality: jpegQuality) { _ in
      image.draw(in: rect)
    }
  }

  func originalSize(_ localID: String?, ret0_: UnsafeMutablePointer<Int64>?) throws {
    ret0_?.pointee = try originals.size(localID ?? "")
  }

  func readOriginal(_ localID: String?, offset: Int64, length: Int64) throws -> Data {
    try originals.read(localID ?? "", offset: offset, length: length)
  }

  private func request(_ asset: PHAsset, side: Int, mode: PHImageRequestOptionsDeliveryMode) -> (UIImage?, [AnyHashable: Any]?) {
    let options = PHImageRequestOptions()
    options.isSynchronous = true
    options.deliveryMode = mode
    options.resizeMode = .exact
    options.version = .current
    options.isNetworkAccessAllowed = false
    var result: (UIImage?, [AnyHashable: Any]?) = (nil, nil)
    PHImageManager.default().requestImage(
      for: asset, targetSize: CGSize(width: side, height: side), contentMode: .aspectFit, options: options
    ) { image, info in
      result = (image, info)
    }
    return result
  }
}

struct OpenFile {
  let fd: Int32
  let size: Int64
}

final class Originals {
  private let lock = NSCondition()
  private let open: (String) throws -> OpenFile
  private let capacity: Int
  private let idle: TimeInterval
  private let retry: TimeInterval
  private var files: [(id: String, file: OpenFile, used: Date)] = []
  private var opening: Set<String> = []
  private var failures: [String: (error: Error, at: Date)] = [:]
  private var timer: DispatchSourceTimer?
  private var warning: NSObjectProtocol?

  init(capacity: Int = 2, idle: TimeInterval = 60, retry: TimeInterval = 10, open: @escaping (String) throws -> OpenFile) {
    self.capacity = capacity
    self.idle = idle
    self.retry = retry
    self.open = open
    let timer = DispatchSource.makeTimerSource(queue: .global(qos: .utility))
    timer.schedule(deadline: .now() + idle / 2, repeating: idle / 2)
    timer.setEventHandler { [weak self] in self?.closeIdle() }
    timer.resume()
    self.timer = timer
    warning = NotificationCenter.default.addObserver(
      forName: UIApplication.didReceiveMemoryWarningNotification, object: nil, queue: nil
    ) { [weak self] _ in
      DispatchQueue.global(qos: .utility).async { self?.closeAll() }
    }
  }

  deinit {
    timer?.cancel()
    if let warning { NotificationCenter.default.removeObserver(warning) }
    closeAll()
  }

  var openCount: Int {
    lock.lock()
    defer { lock.unlock() }
    return files.count
  }

  func size(_ id: String) throws -> Int64 {
    try withFile(id) { $0.size }
  }

  func read(_ id: String, offset: Int64, length: Int64) throws -> Data {
    try withFile(id) { file in
      let count = readLength(offset, length, file.size)
      var data = Data(count: count)
      var done = 0
      try data.withUnsafeMutableBytes { (buffer: UnsafeMutableRawBufferPointer) in
        while done < count {
          let n = pread(file.fd, buffer.baseAddress! + done, count - done, off_t(offset) + off_t(done))
          if n < 0, errno == EINTR { continue }
          if n < 0 { throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno)) }
          if n == 0 { break }
          done += n
        }
      }
      if done < count { data.count = done }
      return data
    }
  }

  func closeIdle(now: Date = Date()) {
    lock.lock()
    defer { lock.unlock() }
    files.removeAll { entry in
      guard now.timeIntervalSince(entry.used) >= idle else { return false }
      close(entry.file.fd)
      return true
    }
  }

  func closeAll() {
    lock.lock()
    defer { lock.unlock() }
    files.forEach { close($0.file.fd) }
    files.removeAll()
  }

  private func withFile<T>(_ id: String, _ body: (OpenFile) throws -> T) throws -> T {
    lock.lock()
    defer { lock.unlock() }
    while opening.contains(id) { lock.wait() }
    let file: OpenFile
    if let i = files.firstIndex(where: { $0.id == id }) {
      file = files.remove(at: i).file
    } else {
      if let failure = failures[id], Date().timeIntervalSince(failure.at) < retry { throw failure.error }
      opening.insert(id)
      lock.unlock()
      let result = Result { try open(id) }
      lock.lock()
      opening.remove(id)
      lock.broadcast()
      switch result {
      case .success(let opened):
        failures[id] = nil
        file = opened
      case .failure(let error):
        failures = failures.filter { Date().timeIntervalSince($0.value.at) < retry }
        failures[id] = (error, Date())
        throw error
      }
    }
    files.append((id, file, Date()))
    while files.count > capacity {
      close(files.removeFirst().file.fd)
    }
    return try body(file)
  }
}

func openFile(_ url: URL) throws -> OpenFile {
  let fd = Darwin.open(url.path, O_RDONLY)
  guard fd >= 0 else { throw NSError(domain: NSPOSIXErrorDomain, code: Int(errno), userInfo: [NSFilePathErrorKey: url.path]) }
  var info = stat()
  guard fstat(fd, &info) == 0 else {
    let code = errno
    close(fd)
    throw NSError(domain: NSPOSIXErrorDomain, code: Int(code))
  }
  return OpenFile(fd: fd, size: Int64(info.st_size))
}

private func openOriginal(_ localID: String) throws -> OpenFile {
  let asset = try fetchAsset(localID)
  let video = asset.mediaType == .video
  if video, let url = (try videoAsset(asset) as? AVURLAsset)?.url, let file = try? openFile(url) {
    return file
  }
  let resources = PHAssetResource.assetResources(for: asset)
  guard let index = servedResource(resources.map(\.type), video: video) else {
    throw mediaError("\(localID) has no original resource")
  }
  return try exportResource(resources[index], localID: localID)
}

func exportResource(_ resource: PHAssetResource, localID: String, timeout: TimeInterval = photoKitTimeout) throws -> OpenFile {
  let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
  let options = PHAssetResourceRequestOptions()
  options.isNetworkAccessAllowed = false
  let lock = NSLock()
  var failure: Error?
  var abandoned = false
  let done = DispatchSemaphore(value: 0)
  PHAssetResourceManager.default().writeData(for: resource, toFile: url, options: options) { error in
    lock.lock()
    failure = error
    let late = abandoned
    lock.unlock()
    if late { try? FileManager.default.removeItem(at: url) }
    done.signal()
  }
  defer { try? FileManager.default.removeItem(at: url) }
  guard done.wait(timeout: .now() + timeout) == .success else {
    lock.lock()
    abandoned = true
    lock.unlock()
    throw mediaError("photo library did not export \(localID) in \(Int(timeout)) s")
  }
  if inCloud(failure) { throw notLocal(localID) }
  if let failure { throw failure }
  return try openFile(url)
}
