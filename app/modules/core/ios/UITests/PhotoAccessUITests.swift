import XCTest

final class PhotoAccessUITests: XCTestCase {
  func testGrantFullPhotoAccessToTestHost() {
    let app = XCUIApplication()
    app.launchEnvironment["PILEUP_REQUEST_PHOTOS"] = "1"
    app.launch()
    let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
    let status = app.staticTexts["photo-status"]
    let labels = ["Allow Full Access", "Allow Access to All Photos", "Разрешить полный доступ", "Разрешить доступ ко всем фото", "Allow"]
    var printed = false
    let deadline = Date().addingTimeInterval(90)
    while Date() < deadline {
      if status.exists, status.label == "3" { return }
      for alert in [springboard.alerts.firstMatch, app.alerts.firstMatch] where alert.waitForExistence(timeout: 1) {
        if !printed {
          print("photo alert tree:\n\(alert.debugDescription)")
          printed = true
        }
        let button = labels.lazy.map { alert.buttons[$0] }.first { $0.exists }
          ?? alert.buttons.matching(NSPredicate(format: "label CONTAINS[c] 'full' OR label CONTAINS[c] 'all photos' OR label CONTAINS[c] 'полный'")).firstMatch
        if button.exists {
          print("tapping \(button.label)")
          button.tap()
        }
      }
    }
    print("springboard tree:\n\(springboard.debugDescription)")
    print("host tree:\n\(app.debugDescription)")
    XCTFail("photo access status: \(status.exists ? status.label : "no label")")
  }
}
