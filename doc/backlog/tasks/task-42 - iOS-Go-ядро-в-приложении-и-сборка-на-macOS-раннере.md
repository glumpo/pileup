---
id: TASK-42
title: 'iOS: Go-ядро в приложении и сборка на macOS-раннере'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-11T01:28:00Z'
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
- [ ] #1 CI на macOS-раннере собирает ядро в xcframework (`make mobile-ios`) и приложение под симулятор iOS без подписи; логи шагов читаются без авторизации в GitHub
- [ ] #2 Swift-модуль даёт тот же JS API, что Android: start, stop, port, token; seed ключа устройства хранится в Keychain без переноса в бэкап и на другое устройство, каталог данных ядра исключён из бэкапа
- [ ] #3 NetworkInfo отдаёт адреса интерфейсов устройства; сотовые интерфейсы исключены
- [ ] #4 Чистая логика Swift покрыта unit-тестами, которые запускаются на macOS-раннере
- [ ] #5 В CI приложение запускается на симуляторе, ядро стартует и отвечает на /health с токеном; без токена — отказ
- [ ] #6 Время сборки и размер приложения с ядром записаны в задаче; решения записаны в architecture.md (A-таблица)
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
