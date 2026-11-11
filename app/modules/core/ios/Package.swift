// swift-tools-version:5.9
import PackageDescription

let package = Package(
  name: "PileupNative",
  platforms: [.iOS("16.4")],
  products: [.library(name: "PileupNative", targets: ["PileupNative"])],
  targets: [
    .binaryTarget(name: "Mobile", path: "Mobile.xcframework"),
    .target(name: "PileupNative", dependencies: ["Mobile"], path: "Native", linkerSettings: [.linkedLibrary("resolv")]),
    .testTarget(name: "PileupNativeTests", dependencies: ["PileupNative"], path: "Tests"),
  ]
)
