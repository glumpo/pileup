# iOS: Go-ядро в приложении, сборка и проверка на macOS-раннере

Описание реализации на ветке `ios/core` после TASK-42 (2026-11-11). Источник истины — код. Решения: A13, A21, A22, A31, A48 в [`architecture.md`](../architecture.md). Задачи: TASK-3 (Android-образец), TASK-42; дальше на эту базу встают TASK-43 (медиатека), TASK-44 (Bonjour и жизненный цикл), TASK-45 (UI), TASK-18 (AirPlay).

## 1. Состав

Всё лежит в `app/modules/core/ios/`. Нативный каталог `app/ios` не коммитится, его создаёт `expo prebuild` (A13).

| Файл | Что |
|---|---|
| `PileupCoreModule.swift` | тонкий Expo-адаптер: JS API и жизненный цикл, единственный файл, который импортирует `ExpoModulesCore` |
| `Native/Core.swift` | старт и стоп фасада `mobile.Start`/`Stop`, `port`, `token`; подготовка каталога данных |
| `Native/KeychainKeystore.swift` | `Keystore`: seed ключа устройства в Keychain |
| `Native/NetworkAddresses.swift` | `NetworkInfo`: адреса интерфейсов через `getifaddrs` |
| `Native/MediaLibrary.swift` | `MediaProvider` и `requestPermissions` — заготовка, наполняет TASK-43 |
| `Native/Renditions.swift` | `RenditionMaker` — заготовка, наполняет TASK-43 |
| `Native/Discovery.swift` | `DiscoveryProvider` — заготовка, наполняет TASK-44 |
| `PileupCore.podspec` | pod модуля: `PileupCoreModule.swift` + `Native/**`, `vendored_frameworks = Mobile.xcframework`, `libresolv` |
| `Package.swift` | SwiftPM-пакет `PileupNative` из `Native/` и binaryTarget `Mobile.xcframework` — только для тестов |
| `TestHost/` | `project.yml` для XcodeGen: приложение-хост и hosted unit-тесты из `Tests/` |
| `Tests/` | XCTest: чистая логика, Keychain, каталог данных, интеграция с ядром |
| `Mobile.xcframework` | результат `make mobile-ios`, не коммитится |

В pod всё (`PileupCoreModule.swift` и `Native/`) компилируется в один модуль `PileupCore`, поэтому адаптер видит типы `Native/` без импорта. В SwiftPM те же файлы — модуль `PileupNative`, тесты подключают его через `@testable import`. Код `Native/` не зависит от Expo, это и даёт быстрый цикл тестов.

## 2. JS API

Тот же, что на Android (`app/modules/core/src/PileupCoreModule.ts`):

- `start(): Promise<void>` — каталог данных `Application Support/core`, Keychain-сервис `com.glumpo.pileup.device`, платформа `"ios"`, версия — `CFBundleShortVersionString` бандла (A31), провайдеры из `Native/`. Повторный `start` при работающем ядре ничего не меняет (A13).
- `stop(): Promise<void>`, `port(): number`, `token(): string` — прямо из фасада.
- `requestMediaPermissions(): Promise<boolean>` — пока всегда `false` без запроса к системе (ключа `NSPhotoLibraryUsageDescription` в Info.plist ещё нет, а вызов PhotoKit без него роняет приложение). Полноценная реализация — TASK-43.

Жизненный цикл: ядро останавливается в `OnDestroy` модуля (перезагрузка JS, уничтожение модуля) и по `UIApplication.willTerminateNotification`. Уход в фон ядро не останавливает: приостановленное приложение система может убить без уведомления, данные ядра это переживают (SQLite WAL). Перезапуск слушающих сокетов после фона — TASK-44.

## 3. Ключ устройства и каталог данных (A21, A48)

- Каталог данных ядра — `Application Support/core` с `isExcludedFromBackup = true` (аналог `noBackupFilesDir` на Android).
- Seed — generic password в Keychain: сервис `com.glumpo.pileup.device`, аккаунт `device-seed`, `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`, без синхронизации. `ThisDeviceOnly` не переносится на другое устройство ни бэкапом, ни миграцией; `AfterFirstUnlock` — ядро может стартовать в фоне после первой разблокировки.
- Отличие от Android: элементы Keychain переживают удаление приложения и восстанавливаются из зашифрованного бэкапа на то же устройство, а каталог данных — нет. Без защиты узел после переустановки или восстановления писал бы под старым ключом новый журнал с seq = 1 — это и есть «продолжить чужой журнал», от которого защищает A21. Поэтому `prepareDataDirectory` удаляет seed из Keychain, если каталога данных ещё нет; ключ живёт ровно столько, сколько каталог.

## 4. NetworkInfo

`getifaddrs`, адреса IPv4 и IPv6 поднятых интерфейсов в числовом виде (`getnameinfo`, `NI_NUMERICHOST`), JSON-массивом строк. Исключены loopback и сотовые `pdp_ip*`. Всё остальное (Wi-Fi `en*`, точка доступа `bridge*`, VPN) остаётся; global unicast, развёртку IPv4-mapped и отброс зон делает Go (A21).

## 5. Как добавить или наполнить провайдера

Провайдер — `final class X: NSObject, MobileXProtocol` в своём файле в `Native/`. Сигнатуры — из заголовка gomobile (`Mobile.xcframework/*/Mobile.framework/Headers/Mobile.objc.h`; на Linux его можно получить `gobind -lang=objc -outdir=/tmp/b ./mobile` в `core/`). Правила импорта в Swift:

- Go-интерфейс `X` становится протоколом `MobileXProtocol` (класс-прокси занимает имя `MobileX`).
- Метод с `error` и nullable-результатом — `throws`: `func media(_ from: Int64, to: Int64) throws -> Data`, `func stop() throws`.
- Метод с `(int64, error)` — `func originalSize(_ localID: String?, ret0_: UnsafeMutablePointer<Int64>?) throws`.
- `(string, error)` с `_Nonnull` результатом не становится `throws`: `func addrs(_ error: NSErrorPointer) -> String`.
- Строки и `Data` в параметрах — опциональные (`String?`, `Data?`).

Провайдеры создаются в `Core.start` без параметров. Если провайдеру нужна зависимость, меняется только эта строка. Новый тест — файл в `Tests/`: XcodeGen подхватывает каталог целиком.

## 6. Сборка

| Цель | Что |
|---|---|
| `make mobile-ios` | `gomobile bind -target=ios -iosversion 16.4` → `app/modules/core/ios/Mobile.xcframework` (ios-arm64, ios-arm64_x86_64-simulator); gomobile закреплён директивой `tool` в `core/go.mod` |
| `make ios-boot` | загрузить симулятор (не ждёт; первая загрузка на раннере — до 1,5 мин миграции данных) |
| `make ios-test` | скачать XcodeGen 2.46.0 в `build/bin` (sha256 проверяется), сгенерировать `TestHost/PileupNativeTests.xcodeproj`, `xcodebuild test` на симуляторе |
| `make ios-prebuild` | `npm ci`, `expo prebuild --platform ios --clean --no-install`, `pod install` |
| `make ios-build` | `xcodebuild` Release под симулятор (JS-бандл внутри, Metro не нужен), только активная архитектура, link map для замеров |
| `make ios-smoke` | `scripts/ios-smoke.sh`: установка и запуск на симуляторе, проверка ядра (раздел 7) |
| `make ios-simulator` | всё подряд, кроме тестов |

Симулятор: `IOS_SIMULATOR` (UDID) или по умолчанию последний iPhone самой новой среды iOS из `simctl list`. Подпись: симуляторные сборки подписываются ad-hoc (`-`), Apple-аккаунт не нужен; сборки под устройство нет (TASK-1).

Тесты идут внутри приложения-хоста, а не как тесты SwiftPM без хоста: у процесса `xctest` на симуляторе нет entitlements, и Keychain отвечает `-34018` (`errSecMissingEntitlement`). Хост — пустое приложение `TestHost`, подписанное ad-hoc, как и само приложение.

## 7. Что доказывает проверка в CI

- `ios-test` (hosted XCTest на симуляторе, 17 тестов): Swift-провайдеры совместимы с протоколами gomobile, xcframework линкуется и ядро работает в iOS-процессе; `Core.start` поднимает ядро, `/health` с токеном — 200, без токена и с чужим — 401; участник pile — `platform = ios` с переданной версией; приглашение создаётся (значит, `NetworkInfo` отдал адрес, который Go признал LAN-адресом); после `Stop` порт закрыт, после нового `Start` — новый токен, старый отвергается, pile и ключ устройства сохранились (seed прочитан из Keychain). Отдельно — правила адресов, атрибуты элемента Keychain (`AfterFirstUnlockThisDeviceOnly`, без синхронизации), исключение каталога из бэкапа и сброс seed без каталога.
- `ios-smoke` (настоящее приложение, Release): после запуска процесс приложения слушает порт на `127.0.0.1` (находится через `lsof` по pid — симулятор делит сеть с хостом), `/health` без токена — 401, приложение живо через 10 с; после `simctl terminate` порт не отвечает; второй запуск снова поднимает ядро на новом порту. Это доказывает, что JS вызвал `start()` через Expo-модуль, pod с xcframework слинкован и ядро стартовало с Keychain в подписанном приложении. Ответ 200 с токеном внутри приложения автоматически не проверяется (токена нет вне процесса); его косвенно показывает скриншот `launch-*.png`: экран «Pileup» без ошибки означает, что `GET /piles` с токеном прошёл. Доставка `willTerminate` при `simctl terminate` не проверяется.

## 8. CI и канал логов

`.github/workflows/ci.yml`, триггер — push во все ветки, кроме `ci-logs`:

- `check` (ubuntu) — `make check`, как раньше.
- `ios-native` (macos-latest): `ios-boot`, `mobile-ios`, `ios-test`. ~5 мин.
- `ios-app` (macos-latest, параллельно): `ios-boot`, `mobile-ios`, `ios-prebuild`, `ios-build`, `ios-smoke`, `sizes`. ~9–10 мин.

Логи заданий публичного репозитория без авторизации недоступны, поэтому задания сами публикуют их в ветку `ci-logs`:

- Каждый шаг идёт через `scripts/ci-step.sh <имя> <команда…>`: вывод — в `build/ci-logs/<имя>.log` (и в консоль Actions), в `steps` — строка `<имя> began=<UTC> exit=<код> seconds=<длительность>`.
- Последний шаг `if: always()` — `scripts/ci-logs.sh <задание> <job.status>`: пишет `status` (`success`/`failure`/`cancelled`) и `run`, кладёт каталог в `<sha>/<run_number>.<run_attempt>/<задание>/` ветки `ci-logs` и пушит с `GITHUB_TOKEN` (`permissions: contents: write`). Логи больше 1 МБ обрезаются до последнего мегабайта. Каталоги разных коммитов, перезапусков и заданий не пересекаются; при гонке пуша скрипт до 10 раз делает fetch, заново кладёт свой каталог поверх свежей ветки и пушит снова (без force и rebase). Пуши с `GITHUB_TOKEN` новых прогонов не запускают.

Чтение (по ssh, без API и без лимита запросов):

```bash
sha=$(git rev-parse HEAD)
until git fetch -q origin ci-logs:refs/remotes/origin/ci-logs 2>/dev/null &&
  git ls-tree -r --name-only origin/ci-logs | grep -q "^$sha/.*/ios-app/status$"; do sleep 30; done
git ls-tree -r --name-only origin/ci-logs | grep "^$sha/"
git show origin/ci-logs:$sha/<run>.<attempt>/ios-app/steps
git show origin/ci-logs:$sha/<run>.<attempt>/ios-app/launch-1.png > /tmp/launch-1.png
```

Если задание упало до последнего шага из-за отмены или сбоя раннера, каталога не будет — тогда `status` не дождаться, нужен таймаут ожидания.

## 9. Замеры (macos-latest: macOS 26.7, Xcode 26.6, arm64, 3 CPU; кеш Go и npm прогрет)

- `make mobile-ios`: 106–169 с (три среза: ios/arm64, iossimulator/arm64, iossimulator/amd64).
- `ios-prebuild` (npm ci + prebuild + pod install): 137–185 с; `ios-build` (Release, arm64): 144–152 с; React Native и модули Expo приходят прекомпилированными (`RCT_USE_PREBUILT_RNCORE`, `EXPO_USE_PRECOMPILED_MODULES`).
- `ios-test`: 87 с при заранее загруженном симуляторе, 251 с без этого.
- Размеры: статическая библиотека ядра — 34,7 МБ (ios-arm64) и 68,2 МБ (универсальная для симулятора, две архитектуры), это архивы с отладочной информацией. Release-приложение под симулятор: `.app` 46,7 МБ, бинарник `app` 14,9 МБ, из них по link map 12,2 МБ — код и данные ядра (без zerofill), 97% бинарника; React Native и Hermes лежат отдельными фреймворками в `.app`.

## 10. Ограничения

- В podspec остался `tvos` из шаблона, а у xcframework нет среза tvOS — сборка модуля под tvOS не пойдёт.
- Нет сборки под устройство и подписи: нужен Apple-аккаунт (TASK-1).
- `requestMediaPermissions` всегда `false`, медиатеки и превью нет (TASK-43); обнаружения нет (TASK-44).
- Ветка `ci-logs` растёт на 1–3 МБ за прогон iOS-заданий; чистить её можно в любой момент, код от неё не зависит.
