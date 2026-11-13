# iOS: Go-ядро в приложении, сборка и проверка на macOS-раннере

Описание реализации на ветке `ios/core` после TASK-42 (2026-11-11, с влитым `master` после TASK-49). Источник истины — код. Решения: A13, A21, A22, A31, A48 в [`architecture.md`](../architecture.md). Задачи: TASK-3 (Android-образец), TASK-42; дальше на эту базу встают TASK-43 (медиатека), TASK-44 (Bonjour и жизненный цикл), TASK-45 (UI), TASK-18 (AirPlay).

## 1. Состав

Всё лежит в `app/modules/core/ios/`. Нативный каталог `app/ios` не коммитится, его создаёт `expo prebuild` (A13).

| Файл | Что |
|---|---|
| `PileupCoreModule.swift` | тонкий Expo-адаптер: JS API, подписка `Lifecycle` в `OnCreate`/`OnDestroy`; единственный файл, который импортирует `ExpoModulesCore` |
| `Native/Core.swift` | старт и стоп фасада `mobile.Start`/`Stop`, `port`, `token`; подготовка каталога данных |
| `Native/KeychainKeystore.swift` | `Keystore`: seed ключа устройства в Keychain |
| `Native/NetworkAddresses.swift` | `NetworkInfo`: адреса интерфейсов через `getifaddrs` |
| `Native/MediaLibrary.swift` | `MediaProvider` и `requestPermissions` — заготовка, наполняет TASK-43 |
| `Native/Renditions.swift` | `RenditionMaker` — заготовка, наполняет TASK-43 |
| `Native/Discovery.swift` | `DiscoveryProvider`: Bonjour (раздел 11) |
| `Native/Lifecycle.swift` | уведомления жизненного цикла: фоновая задача, перезапуск Bonjour, остановка ядра (раздел 11) |
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

Жизненный цикл: ядро останавливается в `OnDestroy` модуля (перезагрузка JS, уничтожение модуля), по `UIApplication.willTerminateNotification` и по истечении фонового времени; при возврате на передний план пересоздаются объявления и поиск Bonjour (раздел 11).

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
| `make ios-build` | `xcodebuild` Release под симулятор (`Pileup.xcworkspace`, схема `Pileup`, продукт `Pileup.app`) (JS-бандл внутри, Metro не нужен), только активная архитектура, link map для замеров |
| `make ios-smoke` | `scripts/ios-smoke.sh`: установка и запуск на симуляторе, проверка ядра (раздел 7); печатает идентификатор и отображаемое имя из `Info.plist` |
| `make ios-simulator` | всё подряд, кроме тестов |

Симулятор: `IOS_SIMULATOR` (UDID) или по умолчанию последний iPhone самой новой среды iOS из `simctl list`. Подпись: симуляторные сборки подписываются ad-hoc (`-`), Apple-аккаунт не нужен; сборки под устройство нет (TASK-1).

Тесты идут внутри приложения-хоста, а не как тесты SwiftPM без хоста: у процесса `xctest` на симуляторе нет entitlements, и Keychain отвечает `-34018` (`errSecMissingEntitlement`). Хост — пустое приложение `TestHost`, подписанное ad-hoc, как и само приложение.

## 7. Что доказывает проверка в CI

- `ios-test` (hosted XCTest на симуляторе, 30 тестов, из них 11 — обнаружение и жизненный цикл (раздел 11)): Swift-провайдеры совместимы с протоколами gomobile, xcframework линкуется и ядро работает в iOS-процессе; `Core.start` поднимает ядро, `/health` с токеном — 200, без токена и с чужим — 401; участник pile — `platform = ios` с переданной версией; приглашение создаётся (значит, `NetworkInfo` отдал адрес, который Go признал LAN-адресом); после `Stop` порт закрыт, после нового `Start` — новый токен, старый отвергается, pile и ключ устройства сохранились (seed прочитан из Keychain); `stop` и сразу `start` подряд несколько раз (так `restartCore()` в JS перезапускает ядро при потере связи) — каждый раз новый токен и `/health` 200; `stop` без ядра и повторный `start` ничего не меняют. Отдельно — правила адресов, атрибуты элемента Keychain (`AfterFirstUnlockThisDeviceOnly`, без синхронизации), исключение каталога из бэкапа и сброс seed без каталога.
- `ios-smoke` (настоящее приложение, Release): после запуска процесс приложения слушает порт на `127.0.0.1` (находится через `lsof` по pid — симулятор делит сеть с хостом), `/health` без токена — 401, приложение живо через 10 с; после `simctl terminate` порт не отвечает; второй запуск снова поднимает ядро на новом порту. Это доказывает, что JS вызвал `start()` через Expo-модуль, pod с xcframework слинкован и ядро стартовало с Keychain в подписанном приложении. Ответ 200 с токеном внутри приложения автоматически не проверяется (токена нет вне процесса); его косвенно показывает скриншот `launch-*.png`: экран «Pileup» без ошибки означает, что `GET /piles` с токеном прошёл. Доставка `willTerminate` при `simctl terminate` не проверяется.

## 8. CI и канал логов

`.github/workflows/ci.yml`, триггер — push во все ветки, кроме `ci-logs`:

- `check` (ubuntu) — `make check`, как раньше, плюс публикация логов.
- `ios-native` (macos-latest): `ios-boot`, `mobile-ios`, `ios-test`. 5–9 мин.
- `ios-app` (macos-latest, параллельно): `ios-boot`, `mobile-ios`, `ios-prebuild`, `ios-build`, `ios-smoke`, `sizes`. 9–16 мин.

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

- `make mobile-ios`: 106–398 с (разброс между раннерами большой) (три среза: ios/arm64, iossimulator/arm64, iossimulator/amd64).
- `ios-prebuild` (npm ci + prebuild + pod install): 137–185 с; `ios-build` (Release, arm64): 144–300 с; React Native и модули Expo приходят прекомпилированными (`RCT_USE_PREBUILT_RNCORE`, `EXPO_USE_PRECOMPILED_MODULES`).
- `ios-test`: 58–206 с при заранее загруженном симуляторе, 251 с без этого.
- Задание целиком (по `began` первого шага и `finished`): `ios-native` 5–9 мин, `ios-app` 9–16 мин; `check` — 2,5 мин на `make check`.
- Размеры: статическая библиотека ядра — 34,7 МБ (ios-arm64) и 68,2 МБ (универсальная для симулятора, две архитектуры), это архивы с отладочной информацией. Release-приложение под симулятор: `.app` 46,7 МБ, бинарник `app` 14,9 МБ, из них по link map 12,2 МБ — код и данные ядра (без zerofill), 97% бинарника; React Native и Hermes лежат отдельными фреймворками в `.app`.

## 10. Ограничения

- В podspec остался `tvos` из шаблона, а у xcframework нет среза tvOS — сборка модуля под tvOS не пойдёт.
- Нет сборки под устройство и подписи: нужен Apple-аккаунт (TASK-1).
- `requestMediaPermissions` всегда `false`, медиатеки и превью нет (TASK-43).
- Ветка `ci-logs` растёт на 1–3 МБ за прогон iOS-заданий; чистить её можно в любой момент, код от неё не зависит.

## 11. Интерфейс на iPhone (TASK-45, A51)

- Имя и идентификатор: `app.json` — `name: Pileup`, `ios.bundleIdentifier` и `android.package` — `com.glumpo.pileup`. Prebuild создаёт `app/ios/Pileup.xcworkspace`, схему и продукт `Pileup`; от этого зависят `IOS_APP` и `ios-build` в `Makefile` и `scripts/ios-sizes.sh` (`Pileup.app/Pileup`, `Pileup-LinkMap-*`). Путь APK не меняется (модуль Gradle по-прежнему `app`). Тексты разрешений: камера — плагин `expo-camera` в `app.json`, медиатека и локальная сеть — `app.plugin.js` модуля ядра.
- Безопасные области: `react-native-safe-area-context`; `SafeAreaProvider` с `initialWindowMetrics` в `App.tsx`, `Safe` в `ui.tsx` — отступ сверху `inset.top + 12`, снизу `inset.bottom` (вместо прежних постоянных 48 сверху); через него идут `Screen`, экран запуска ядра, ошибка запуска и запасной экран `Boundary`. Просмотр: подпись сверху — `inset.top + 12`, текст проблемы снизу — `inset.bottom + 16`; сами фото и видео занимают весь экран. Отступы слева и справа не учитываются: ориентация только портретная.
- Назад: на каждом экране, кроме списка pile и пульта, — «‹ Назад» в шапке `Screen` или «Закрыть» (`✕`) в просмотре; `BackHandler` на iOS ничего не делает, на Android остаётся. С пульта — только «Остановить показ» (A25). Жеста «свайп от края» нет.
- Клавиатура: `automaticallyAdjustKeyboardInsets` у списков с полями ввода (список pile, вход по приглашению, «Мои файлы»); у полей дат — клавиатура `numbers-and-punctuation` (на Android значение игнорируется).
- Тесты: `npm test` запускает каждый тест дважды — проекты jest `iOS` (`jest-expo/ios`) и `Android` (`jest-expo/android`), `Platform.OS` и выбор `*.ios.*`/`*.android.*` как на устройстве; один проект — `npx jest --selectProjects iOS`. Отличия iPhone — `test/iphone.test.tsx` (безопасные области — штатный мок `react-native-safe-area-context` с подменой `initialWindowMetrics`).
- Не проверено без iPhone: реальные вырезы и Dynamic Island разных моделей (кроме симулятора дымового запуска), индикатор Home на экранах со списками, сдвиг полей над клавиатурой, отсутствие конфликта системного жеста «назад» и листания (жеста нет — проверить, что листание просмотра не открывает ничего лишнего), чтение «Закрыть» в VoiceOver, экспорт журнала через системное окно «Поделиться». Как проверить: на iPhone с Dynamic Island и на iPhone SE пройти все экраны — шапка ниже выреза, последняя кнопка выше индикатора Home; на «Pileup» с несколькими pile ввести название и имя — поле видно над клавиатурой; в «Моих файлах» клавиатура дат с цифрами и дефисом; в «Журнале» «Экспортировать» открывает окно «Поделиться» с текстом журнала.

## 12. Обнаружение и жизненный цикл (TASK-44, A22, A50)

### Discovery

Один экземпляр на процесс (`Core.discovery`), его получает каждый `MobileStart`; `Stop` ядра очищает его полностью. Всё состояние — на последовательной очереди `com.glumpo.pileup.discovery`; `advertise`/`browse` ставят работу в очередь, `stop` ждёт её (после возврата `stop` обратных вызовов в Go нет).

- **Объявление:** `DNSServiceRegister(name, "_pileup._tcp", port, txt)` с `DNSServiceSetDispatchQueue`. Порт слушает Go, dns-sd свой сокет не открывает. TXT из Go уже в формате DNS-SD и уходит байтами как есть. Ошибка регистрации (сразу или в обратном вызове) — в журнал.
- **Поиск:** `NWBrowser(.bonjourWithTXTRecord)` на каждый из трёх типов. Набор результатов сводится по имени экземпляра (`Results`): новое имя — разрешить; имя пропало из всех результатов (все интерфейсы) — `Lost`; изменился TXT — разрешить заново.
- **Разрешение:** `DNSServiceResolve` (хост, порт из SRV, TXT байтами) → `DNSServiceGetAddrInfo` только IPv4 → `Found(name, "a.b.c.d", port, txt)`. Таймаут 10 с, ошибки и таймаут — в журнал (`discovery`, `dns-sd: …`). IPv6 не берётся: Android тоже даёт IPv4, а link-local IPv6 без зоны бесполезен.
- **Запрет «Локальной сети»:** `NWBrowser.State` `.waiting`/`.failed` с `NWError.dns(kDNSServiceErr_PolicyDenied)` → `handler.denied(true)`, `.ready` → `denied(false)` (`localNetworkDenied`). Прочие `waiting`/`failed` — в журнал. В ядре признак общий для узла, последнее сообщение любого из трёх поисков выигрывает (A50).
- **`restart()`:** снять все регистрации, поиски и незавершённые разрешения и создать их заново из запомненных. Найденные до перезапуска имена помечаются устаревшими: первый же набор результатов после перезапуска разрешает всё присутствующее заново (адрес мог смениться) и даёт `Lost` для пропавших.

### Lifecycle и граница с JS

| Событие | Натив (`Lifecycle`) | JS (`App.tsx`, A54) |
|---|---|---|
| `didEnterBackground` | `beginBackgroundTask` | — |
| истекло фоновое время | `Core.stop()` синхронно на главном потоке, `endBackgroundTask` | — |
| `willEnterForeground` | `endBackgroundTask`; `Core.discovery.restart()` (если ядро остановлено — ничего не делает) | — |
| `AppState active` | — | `GET /health` (2 с): ответило → `POST /reconnect` (порты pile самоподключением, паузы подключений, A55, A36); нет → `stop` + `start` |
| `willTerminate` | `Core.stop()` | — |

Ядро запускает только JS, натив его лишь останавливает, поэтому двух перезапусков нет. Уведомления и `AppState` приходят на главном потоке, истечение фоновой задачи — тоже, а фоновая задача завершается в `willEnterForeground`: остановка по истечению не может начаться после возврата на передний план. Фоновая задача берётся на любой уход в фон, без признака «идёт раздача» (TASK-58): обычные фоновые режимы (audio, location) не используются.

Почему ядро останавливается по истечении (V5 п. 2, вопрос TASK-49): участники и экран получают FIN сразу, а не ждут 10 с простоя и 5 с подключения; к моменту приостановки слушающих сокетов уже нет, так что `accept` на мёртвом сокете (`ECONNABORTED`) не может зациклиться. Если `beginBackgroundTask` не дал времени (`.invalid`), приложение приостанавливается с работающим ядром — тогда при возврате остаётся путь A54/A55.

От TASK-58 натив ничего не требует. Признак «сейчас отдаю оригинал» нужен UI (не гасить экран и баннер «Не блокируйте экран, идёт раздача», V5 п. 1); если продукт решит не перезапускать ядро после фона без раздачи, условие `Core.stop()` в `Lifecycle.expired` можно будет взять оттуда.

### Признак запрета в приложении

`GET /local-network` → `{"denied": bool}`; компонент `src/LocalNetwork.tsx` (`LocalNetworkBanner`) опрашивает его при монтировании и раз в 3 с и показывает баннер с кнопкой «Открыть Настройки» (`Linking.openSettings()`, страница Pileup в Настройках). Подключён в `PileScreen.tsx`, `Join.tsx`, `ScreenPicker.tsx` первой строкой внутри `Screen`. Ошибка входа при запрете — `local_network_denied` (текст в `ui.tsx`). На Android признак всегда `false`.

### ATS

`make ios-prebuild` после `expo prebuild` проверяет `NSAppTransportSecurity` в `app/ios/Pileup/Info.plist`: ровно `NSAllowsArbitraryLoads = false` и `NSAllowsLocalNetworking = true`, без исключений доменов; словарь печатается в `ios-prebuild.log`. `NSAllowsLocalNetworking` разрешает `http://127.0.0.1` (UI API и картинки через `expo-image`), остальной cleartext запрещён.

### Тесты на симуляторе

- `DiscoveryLogicTests`: сведение результатов по имени, `Lost` по последнему результату, повторное разрешение при смене TXT и после перезапуска, `PolicyDenied`/`ready` → признак.
- `DiscoveryTests` (тип `_pileuptest._tcp`, два экземпляра в одном процессе, mDNSResponder хоста): найдено с IPv4, портом и TXT байт в байт, `denied(false)`; `Lost` после `stop`; перезапущенный поиск находит снова без `Lost`; перезапущенное объявление находит новый поиск; после `stop` `restart` ничего не возвращает.
- `LifecycleTests`: короткий фон не трогает ядро (тот же порт, `/health` 200); истечение фона останавливает ядро (порт не отвечает), возврат на передний план ядро не запускает.
- mDNS на macOS-раннере работает: симулятор ходит через mDNSResponder хоста, `DiscoveryTests` идут ~10 с.
- `TestHost` объявляет `NSLocalNetworkUsageDescription` и `NSBonjourServices` (XcodeGen `info`, файл `TestHost/Info.plist` генерируется).

### Не проверено без iPhone и как проверить

На симуляторе нет запроса «Локальная сеть» и приостановки как на устройстве. На устройстве (сборка с подписью, TASK-1), журнал — «Журнал» → «Поделиться»:

1. **Запрет:** при первом запуске отклонить запрос (или Настройки → Pileup → «Локальная сеть» выкл.). Ожидается событие `discovery` «local network access denied: true», баннер на экране pile, входа и выбора экрана, вход по приглашению — «Нет доступа к локальной сети…». Включить в Настройках и вернуться: баннер пропадает за ≤3 с (если iOS перезапускает приложение при смене разрешения — после запуска). Если признак не появляется — смотреть в журнале `dns-sd: browse …` с кодом ошибки.
2. **Возврат из фона < 30 с:** заблокировать iPhone на 10 с, разблокировать. Второй телефон в pile: участник «на связи» без перезапуска; экран Cast в списке; в журнале iPhone «reconnect, requested by the app».
3. **Фон > 30 с во время раздачи видео:** показ видео с iPhone на Cast, у iPhone — домашний экран. Через ~30 с на ТВ постер почти сразу после остановки (без паузы ~15 с), у участников iPhone «нет связи». Возврат: ядро перезапускается приложением (A54), pile и участники на месте, в журнале событие запуска «Pileup … ios started».
4. **ECONNABORTED (TASK-49):** если фоновая задача не дала времени (мало заряда/режим энергосбережения), заблокировать на минуту и вернуться: `POST /reconnect` должен ответить, в журнале «port … does not accept connections, reopening» и затем входящие подключения проходят; если `/health` не отвечает — приложение само перезапускает ядро (A54). Признак зацикливания — 100% CPU у приложения в фоне в Xcode Instruments и пустой журнал после возврата.
