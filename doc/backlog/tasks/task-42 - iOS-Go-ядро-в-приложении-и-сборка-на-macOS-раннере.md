---
id: TASK-42
title: 'iOS: Go-ядро в приложении и сборка на macOS-раннере'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-11T21:45:00Z'
labels:
  - ios
  - app
  - core
  - ci
milestone: m-0
dependencies: []
priority: high
type: feature
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
iOS-часть TASK-3 не сделана: Swift-модуль `app/modules/core/ios` — заглушка шаблона, ядро не подключено, сборки под iOS нет. Mac нет, есть только macOS-раннеры GitHub; логи заданий публичного репозитория без авторизации недоступны (API отвечает 403), поэтому раннер должен сам отдавать логи туда, где их можно прочитать по ssh. Без рабочего цикла «push → сборка → логи» остальные iOS-задачи (медиатека, обнаружение, AirPlay) вести нельзя. Образец — Android-часть TASK-3 и решения A13, A21, A31.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 CI на macOS-раннере собирает ядро в xcframework (`make mobile-ios`) и приложение под симулятор iOS без подписи; логи шагов читаются без авторизации в GitHub
- [x] #2 Swift-модуль даёт тот же JS API, что Android: start, stop, port, token; seed ключа устройства хранится в Keychain без переноса в бэкап и на другое устройство, каталог данных ядра исключён из бэкапа
- [x] #3 NetworkInfo отдаёт адреса интерфейсов устройства; сотовые интерфейсы исключены
- [x] #4 Чистая логика Swift покрыта unit-тестами, которые запускаются на macOS-раннере
- [x] #5 В CI приложение запускается на симуляторе, ядро стартует и отвечает на /health с токеном; без токена — отказ
- [x] #6 Время сборки и размер приложения с ядром записаны в задаче; решения записаны в architecture.md (A-таблица)
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. CI-канал логов: scripts/ci-step.sh (лог и время шага в каталог), scripts/ci-logs.sh (коммит логов и статуса в ветку ci-logs: <sha>/<run>.<attempt>/<job>/, повтор с fetch при гонке); ci-logs исключена из триггера → проверка: первый прогон виден через git fetch origin ci-logs.
2. make mobile-ios: gomobile bind -target=ios → app/modules/core/ios/Mobile.xcframework (gitignored).
3. Swift: Native/ — Core (каталог данных с исключением из бэкапа, старт/стоп фасада), KeychainKeystore (AfterFirstUnlockThisDeviceOnly), NetworkAddresses (getifaddrs, без pdp_ip*), заготовки MediaLibrary, Renditions, Discovery — по файлу на провайдера; PileupCoreModule.swift — тонкий Expo-адаптер (start/stop/port/token/requestMediaPermissions, остановка в OnDestroy и willTerminate). Package.swift (SwiftPM) с binaryTarget xcframework для быстрых тестов; podspec — vendored_frameworks.
4. Тесты XCTest в Package (make ios-test на симуляторе): адреса, Keychain, каталог/бэкап, интеграция: Start → /health 200 с токеном, 401 без, Stop, перезапуск, platform ios.
5. Задание ios-app: make mobile-ios, prebuild, pod install, xcodebuild Release под симулятор, запуск на симуляторе (scripts/ios-smoke.sh: порт ядра через lsof, /health без токена → 401, скриншот), размеры .app и вклад ядра по link map.
6. Замеры в заметки, A48, doc/adr/ios.md, README; локально go test, tsc, jest, make check-часть.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
## Ход работы (2026-11-11)

Сделано: канал логов CI через ветку ci-logs (scripts/ci-step.sh, scripts/ci-logs.sh; каталог <sha>/<run>.<attempt>/<задание>/, повтор пуша с fetch, ci-logs исключена из триггера, логи check тоже публикуются); make mobile-ios (gomobile bind -target=ios -iosversion 16.4 → app/modules/core/ios/Mobile.xcframework); Swift: PileupCoreModule.swift (Expo-адаптер) + Native/ (Core, KeychainKeystore, NetworkAddresses, заготовки MediaLibrary, Renditions, Discovery — по файлу); podspec с vendored_frameworks и libresolv; Package.swift + TestHost (XcodeGen 2.46.0, sha256) для hosted XCTest; scripts/ios-smoke.sh, scripts/ios-sizes.sh; doc/adr/ios.md, A48.

Находки: тесты SwiftPM без хоста на симуляторе получают -34018 от Keychain — тесты перенесены в приложение-хост. brew install xcodegen — 3 мин, заменён скачиванием релиза. Первая загрузка симулятора — до 1,5 мин (миграция данных), поэтому make ios-boot идёт первым шагом. Keychain переживает удаление приложения → seed удаляется, если нет каталога данных (A48).

После слияния master (TASK-47…49, restartCore() = stop() + start()): добавлены тесты stop→start подряд; зелёные.

## Замеры (macos-latest: macOS 26.7.2, Xcode 26.6, arm64, 3 CPU, iOS 26.2–26.5)
- make mobile-ios: 106–398 с (разброс раннеров).
- ios-prebuild (npm ci + prebuild + pod install): 137–185 с; ios-build (Release, симулятор arm64): 144–300 с — RN и модули Expo прекомпилированы.
- ios-test: 58–206 с при загруженном симуляторе (251 с без); ios-smoke: 47–79 с.
- Задание ios-native 5–9 мин, ios-app 9–16 мин, параллельно.
- Ядро: статическая библиотека ios-arm64 34,7 МБ, симулятор (arm64+x86_64) 68,2 МБ (архивы с отладочной информацией). Release .app под симулятор 46,7 МБ, бинарник app 14,9 МБ, из них ядро по link map 12,25 МБ (97% бинарника; RN и Hermes — отдельные фреймворки). Сборки без ядра для сравнения не делалось: вклад взят из link map.

Доказательства: прогон 40 (коммит 7e1c284), ci-logs: 7e1c284a…/40.1/{check,ios-native,ios-app}/ — все success; 19 XCTest прошли; smoke: два запуска, /health без токена 401, после terminate порт закрыт; скриншот launch-*.png — экран Pileup без ошибки.
Не проверено: доставка willTerminate (simctl terminate убивает процесс); 200 с токеном внутри настоящего приложения автоматически (есть в hosted-тесте и косвенно по скриншоту); сборка под устройство (нет аккаунта Apple).

Лид: слито в master (0139e6e). На master после слияния все три задания CI зелёные (ci-logs: 0139e6e…, 8f03874…), в том числе сборка приложения под симулятор с expo-image (TASK-56).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
iOS-часть ядра и цикл CI на macOS-раннере. Swift-модуль app/modules/core/ios: Expo-адаптер PileupCoreModule.swift (start/stop/port/token/requestMediaPermissions, стоп в OnDestroy и по willTerminate) и независимый от Expo Native/ (Core, KeychainKeystore с AfterFirstUnlockThisDeviceOnly и сбросом seed без каталога данных, каталог Application Support/core вне бэкапа, NetworkAddresses без pdp_ip*, заготовки MediaLibrary/Renditions/Discovery по файлу). make mobile-ios собирает Mobile.xcframework (vendored в podspec). CI: ios-native (xcframework + 19 hosted XCTest на симуляторе, включая /health 200/401 и stop→start подряд) и ios-app (prebuild, pod install, Release под симулятор, запуск приложения: порт ядра по lsof, /health без токена 401, перезапуск, скриншот, размеры по link map). Логи всех заданий — в ветке ci-logs (doc/adr/ios.md §8). Решения — A48. Проверено прогоном 40 на 7e1c284 (check, ios-native, ios-app — success), локально go test, tsc, jest. Риски/не проверено: 200 с токеном в настоящем приложении — только косвенно (скриншот), доставка willTerminate, сборка под устройство (нужен аккаунт Apple), tvos в podspec без среза.
<!-- SECTION:FINAL_SUMMARY:END -->
