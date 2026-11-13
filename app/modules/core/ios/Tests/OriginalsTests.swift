import UIKit
import XCTest
@testable import PileupNative

final class OriginalsTests: XCTestCase {
  private var dir: URL!
  private var contents: [String: Data] = [:]
  private let lock = NSLock()
  private var opened: [String] = []

  override func setUpWithError() throws {
    dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    for (name, size) in [("a", 300_001), ("b", 10), ("c", 4096)] {
      let data = Data((0..<size).map { UInt8(truncatingIfNeeded: $0 * 31 + Int(name.unicodeScalars.first!.value)) })
      try data.write(to: dir.appendingPathComponent(name))
      contents[name] = data
    }
  }

  override func tearDownWithError() throws {
    try? FileManager.default.removeItem(at: dir)
  }

  private func originals(idle: TimeInterval = 60, retry: TimeInterval = 10) -> Originals {
    Originals(idle: idle, retry: retry) { [unowned self] id in
      self.lock.lock()
      self.opened.append(id)
      self.lock.unlock()
      if id.hasPrefix("slow") { Thread.sleep(forTimeInterval: 2) }
      return try openFile(self.dir.appendingPathComponent(String(id.dropFirst(id.hasPrefix("slow") ? 4 : 0))))
    }
  }

  private func elapsed(_ body: () -> Void) -> TimeInterval {
    let started = Date()
    body()
    return Date().timeIntervalSince(started)
  }

  func testRangesReturnFileBytes() throws {
    let files = originals()
    let a = contents["a"]!
    XCTAssertEqual(try files.size("a"), Int64(a.count))
    var read = Data()
    while true {
      let chunk = try files.read("a", offset: Int64(read.count), length: 65_536)
      if chunk.isEmpty { break }
      XCTAssertLessThanOrEqual(chunk.count, 65_536)
      read.append(chunk)
    }
    XCTAssertEqual(read, a)
    XCTAssertEqual(try files.read("a", offset: 7, length: 1000), a.subdata(in: 7..<1007))
    XCTAssertEqual(try files.read("a", offset: Int64(a.count) - 5, length: 1 << 20), a.suffix(5))
    XCTAssertEqual(try files.read("a", offset: Int64(a.count) + 10, length: 100), Data())
    XCTAssertEqual(opened, ["a"])
  }

  func testParallelReadsShareOneOpenFile() throws {
    let files = originals()
    let a = contents["a"]!
    let chunks = 30
    let size = a.count / chunks + 1
    let results = NSMutableArray(array: Array(repeating: Data(), count: chunks))
    DispatchQueue.concurrentPerform(iterations: chunks) { i in
      let data = (try? files.read("a", offset: Int64(i * size), length: Int64(size))) ?? Data()
      objc_sync_enter(results)
      results[i] = data
      objc_sync_exit(results)
    }
    XCTAssertEqual((results as! [Data]).reduce(Data(), +), a)
    XCTAssertEqual(opened, ["a"])
  }

  func testAtMostTwoFilesStayOpenLeastRecentlyUsedIsClosed() throws {
    let files = originals()
    _ = try files.size("a")
    _ = try files.size("b")
    _ = try files.size("a")
    _ = try files.size("c")
    XCTAssertEqual(files.openCount, 2)
    _ = try files.size("a")
    XCTAssertEqual(opened, ["a", "b", "c"])
    _ = try files.size("b")
    XCTAssertEqual(opened, ["a", "b", "c", "b"])
    XCTAssertEqual(try files.read("b", offset: 0, length: 100), contents["b"])
  }

  func testIdleFilesAreClosed() throws {
    let files = originals(idle: 60)
    _ = try files.size("a")
    files.closeIdle(now: Date().addingTimeInterval(30))
    XCTAssertEqual(files.openCount, 1)
    files.closeIdle(now: Date().addingTimeInterval(61))
    XCTAssertEqual(files.openCount, 0)
    XCTAssertEqual(try files.read("a", offset: 0, length: 3), contents["a"]!.prefix(3))
    XCTAssertEqual(opened, ["a", "a"])
  }

  func testIdleTimerClosesFilesByItself() throws {
    let files = originals(idle: 0.2)
    _ = try files.size("a")
    let closed = expectation(description: "closed")
    DispatchQueue.global().asyncAfter(deadline: .now() + 1) {
      if files.openCount == 0 { closed.fulfill() }
    }
    wait(for: [closed], timeout: 3)
  }

  func testMemoryWarningClosesFiles() throws {
    let files = originals()
    _ = try files.size("a")
    _ = try files.size("b")
    NotificationCenter.default.post(name: UIApplication.didReceiveMemoryWarningNotification, object: nil)
    XCTAssertTrue(eventually(2) { files.openCount == 0 })
  }

  func testSlowOpenDoesNotDelayOtherFilesOrTheMainThread() throws {
    let files = originals()
    XCTAssertEqual(try files.read("b", offset: 0, length: 100), contents["b"])
    let opened = expectation(description: "slow file opened")
    DispatchQueue.global().async {
      XCTAssertEqual(try? files.size("slowa"), Int64(self.contents["a"]!.count))
      opened.fulfill()
    }
    Thread.sleep(forTimeInterval: 0.3)
    XCTAssertLessThan(elapsed { XCTAssertEqual(try? files.read("b", offset: 0, length: 100), contents["b"]) }, 0.5)
    XCTAssertLessThan(elapsed { XCTAssertEqual(try? files.size("c"), 4096) }, 0.5)
    XCTAssertLessThan(elapsed { files.closeIdle() }, 0.5)
    XCTAssertLessThan(elapsed {
      NotificationCenter.default.post(name: UIApplication.didReceiveMemoryWarningNotification, object: nil)
    }, 0.5)
    wait(for: [opened], timeout: 5)
  }

  func testParallelRequestsForASlowFileOpenItOnce() throws {
    let files = originals()
    DispatchQueue.concurrentPerform(iterations: 4) { _ in
      XCTAssertEqual(try? files.read("slowb", offset: 0, length: 100), self.contents["b"])
    }
    XCTAssertEqual(opened, ["slowb"])
  }

  func testFailedOpenIsReturnedAndNotRetriedAtOnce() throws {
    let files = originals(retry: 0.5)
    XCTAssertThrowsError(try files.size("missing"))
    XCTAssertThrowsError(try files.read("missing", offset: 0, length: 1))
    XCTAssertEqual(opened, ["missing"])
    XCTAssertEqual(files.openCount, 0)
    Thread.sleep(forTimeInterval: 0.6)
    XCTAssertThrowsError(try files.size("missing"))
    XCTAssertEqual(opened, ["missing", "missing"])
  }
}
