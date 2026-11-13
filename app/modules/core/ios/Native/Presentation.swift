import AVFoundation
import Mobile
import UIKit

struct Slide: Equatable {
  let asset: String
  let image: URL
  let video: URL?
  let playing: Bool
  let around: [URL]

  init?(_ value: [String: Any]?) {
    guard let value, let asset = value["asset"] as? String,
      let image = (value["image"] as? String).flatMap(URL.init(string:))
    else { return nil }
    self.asset = asset
    self.image = image
    video = (value["video"] as? String).flatMap(URL.init(string:))
    playing = value["playing"] as? Bool ?? false
    around = (value["around"] as? [String] ?? []).compactMap(URL.init(string:))
  }

  var images: Set<URL> { Set([image] + around) }
}

func playerState(_ status: AVPlayer.TimeControlStatus) -> String? {
  switch status {
  case .playing: return "playing"
  case .waitingToPlayAtSpecifiedRate: return "buffering"
  default: return nil
  }
}

func playbackFailure(_ error: Error?) -> (reason: String?, message: String) {
  guard let error = error as NSError? else { return (nil, "") }
  let errors = [error] + [error.userInfo[NSUnderlyingErrorKey] as? NSError].compactMap { $0 }
  let message = errors.map { "\($0.domain) \($0.code)" }.joined(separator: " / ") + ": " + error.localizedDescription
  let source = [AVError.Code.serverIncorrectlyConfigured, .contentIsUnavailable, .noLongerPlayable].map(\.rawValue)
  let format = [AVError.Code.decodeFailed, .fileFormatNotRecognized, .failedToParse, .decoderNotFound, .decoderTemporarilyUnavailable, .operationNotSupportedForAsset].map(\.rawValue)
  if errors.contains(where: { $0.domain == NSURLErrorDomain || ($0.domain == AVFoundationErrorDomain && source.contains($0.code)) }) {
    return ("network", message)
  }
  if errors.contains(where: { $0.domain == AVFoundationErrorDomain && format.contains($0.code) }) {
    return ("unsupported", message)
  }
  return (nil, message)
}

final class Presentation {
  static let shared = Presentation()

  var onDisplay: (Bool) -> Void = { _ in }
  var onPlayer: ([String: String]) -> Void = { _ in }

  private(set) var slide: Slide?
  let player = AVPlayer()
  private var screens: [PresentationController] = []
  private var images: [URL: UIImage] = [:]
  private var loading: Set<URL> = []
  private var failed: Set<URL> = []
  private let session = URLSession(configuration: .ephemeral)
  private var video: URL?
  private var reported: String?
  private var observers: [NSKeyValueObservation] = []
  private var end: NSObjectProtocol?
  private var resumes: [NSObjectProtocol] = []

  init() {
    player.allowsExternalPlayback = true
    player.usesExternalPlaybackWhileExternalScreenIsActive = true
    let center = NotificationCenter.default
    resumes = [
      center.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.refresh() },
      center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { [weak self] note in
        if note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt == AVAudioSession.InterruptionType.ended.rawValue { self?.refresh() }
      },
    ]
  }

  deinit {
    resumes.forEach { NotificationCenter.default.removeObserver($0) }
  }

  var connected: Bool { !screens.isEmpty }

  func attach(_ screen: PresentationController) {
    screens.append(screen)
    screen.playerLayer.player = player
    MobileReport("renderer", "external display connected")
    if screens.count == 1 { onDisplay(true) }
    refresh()
  }

  func detach(_ screen: PresentationController) {
    screens.removeAll { $0 === screen }
    screen.playerLayer.player = nil
    MobileReport("renderer", "external display disconnected")
    if screens.isEmpty { onDisplay(false) }
    refresh()
  }

  func show(_ slide: Slide?) {
    self.slide = slide
    refresh()
  }

  private func refresh() {
    guard connected, let slide else {
      images = [:]
      failed = []
      play(nil, asset: "")
      draw()
      return
    }
    let keep = slide.images
    images = images.filter { keep.contains($0.key) }
    failed.formIntersection(keep)
    for url in [slide.image] + slide.around where images[url] == nil { load(url) }
    play(slide.video, asset: slide.asset)
    if slide.video != nil {
      if slide.playing { player.play() } else { player.pause() }
    }
    draw()
  }

  private func draw() {
    for screen in screens {
      guard let slide else {
        screen.show(nil, idle: true)
        continue
      }
      if let image = images[slide.image] {
        screen.show(image, idle: false)
      } else {
        screen.show(failed.contains(slide.image) ? nil : screen.imageView.image, idle: false)
      }
    }
  }

  private func load(_ url: URL) {
    guard loading.insert(url).inserted else { return }
    session.dataTask(with: url) { data, response, _ in
      let ok = (response as? HTTPURLResponse)?.statusCode == 200
      let image = ok ? data.flatMap(UIImage.init(data:))?.preparingForDisplay() : nil
      DispatchQueue.main.async { self.loaded(url, image) }
    }.resume()
  }

  private func loaded(_ url: URL, _ image: UIImage?) {
    loading.remove(url)
    guard connected, let slide, slide.images.contains(url) else { return }
    if let image {
      images[url] = image
      failed.remove(url)
    } else if url == slide.image {
      failed.insert(url)
      DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [weak self] in
        if let self, self.connected, self.slide?.image == url { self.load(url) }
      }
    }
    if url == slide.image { draw() }
  }

  private func play(_ url: URL?, asset: String) {
    guard url != video else { return }
    video = url
    reported = nil
    observers = []
    if let end { NotificationCenter.default.removeObserver(end) }
    end = nil
    guard let url else {
      player.replaceCurrentItem(with: nil)
      try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
      return
    }
    try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
    let item = AVPlayerItem(url: url)
    let report: (String, [String: String]) -> Void = { [weak self] state, details in
      DispatchQueue.main.async {
        guard let self, self.video == url, self.reported != state else { return }
        self.reported = state
        self.onPlayer(["asset": asset, "video": url.absoluteString, "state": state].merging(details) { a, _ in a })
      }
    }
    observers = [
      item.observe(\.status) { item, _ in
        guard item.status == .failed else { return }
        let (reason, message) = playbackFailure(item.error)
        report("failed", ["message": message].merging(reason.map { ["reason": $0] } ?? [:]) { a, _ in a })
      },
      player.observe(\.timeControlStatus) { player, _ in
        if let state = playerState(player.timeControlStatus) { report(state, [:]) }
      },
    ]
    end = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: item, queue: .main) { _ in report("ended", [:]) }
    player.replaceCurrentItem(with: item)
  }
}

final class PresentationController: UIViewController {
  let imageView = UIImageView()
  let idleLabel = UILabel()
  let playerLayer = AVPlayerLayer()

  override func viewDidLoad() {
    super.viewDidLoad()
    view.backgroundColor = .black
    imageView.frame = view.bounds
    imageView.autoresizingMask = [.flexibleWidth, .flexibleHeight]
    imageView.contentMode = .scaleAspectFit
    view.addSubview(imageView)
    playerLayer.videoGravity = .resizeAspect
    view.layer.addSublayer(playerLayer)
    idleLabel.text = "Pileup готов к показу.\nНа iPhone откройте pile и нажмите «Показать на экране»."
    idleLabel.textColor = .white
    idleLabel.font = .systemFont(ofSize: 40, weight: .medium)
    idleLabel.numberOfLines = 0
    idleLabel.textAlignment = .center
    idleLabel.frame = view.bounds.insetBy(dx: 80, dy: 80)
    idleLabel.autoresizingMask = [.flexibleWidth, .flexibleHeight]
    view.addSubview(idleLabel)
  }

  override func viewDidLayoutSubviews() {
    super.viewDidLayoutSubviews()
    playerLayer.frame = view.bounds
  }

  func show(_ image: UIImage?, idle: Bool) {
    imageView.image = image
    idleLabel.isHidden = !idle
  }
}

@objc(PileupPresentationSceneDelegate)
final class PresentationSceneDelegate: UIResponder, UIWindowSceneDelegate {
  var window: UIWindow?
  private var screen: PresentationController?

  func scene(_ scene: UIScene, willConnectTo session: UISceneSession, options connectionOptions: UIScene.ConnectionOptions) {
    guard let scene = scene as? UIWindowScene else { return }
    let screen = PresentationController()
    let window = UIWindow(windowScene: scene)
    window.rootViewController = screen
    window.isHidden = false
    self.window = window
    self.screen = screen
    Presentation.shared.attach(screen)
  }

  func sceneDidDisconnect(_ scene: UIScene) {
    if let screen { Presentation.shared.detach(screen) }
    screen = nil
    window = nil
  }
}
