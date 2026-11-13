# iOS: Go-ядро в приложении, сборка и проверка на macOS-раннере

Описание реализации после TASK-42 и TASK-43 (медиатека, ветка `ios/media`, 2026-11-11). Источник истины — код. Решения: A13, A21, A22, A31, A48, A49 в [`architecture.md`](../architecture.md). Задачи: TASK-3 (Android-образец), TASK-42; дальше на эту базу встают TASK-43 (медиатека), TASK-44 (Bonjour и жизненный цикл), TASK-45 (UI), TASK-18 (AirPlay).

## 1. Состав

Всё лежит в `app/modules/core/ios/`. Нативный каталог `app/ios` не коммитится, его создаёт `expo prebuild` (A13).

| Файл | Что |
|---|---|
| `PileupCoreModule.swift` | тонкий Expo-адаптер: JS API, подписка `Lifecycle` в `OnCreate`/`OnDestroy`; единственный файл, который импортирует `ExpoModulesCore` |
| `Native/Core.swift` | старт и стоп фасада `mobile.Start`/`Stop`, `port`, `token`; подготовка каталога данных |
| `Native/KeychainKeystore.swift` | `Keystore`: seed ключа устройства в Keychain |
| `Native/NetworkAddresses.swift` | `NetworkInfo`: адреса интерфейсов через `getifaddrs` |
| `Native/Media.swift` | чистая логика медиатеки: JSON `node.MediaItem`, интервал запроса, выбор ресурса оригинала, имя и MIME, размеры T1/T2, разрешения, ошибка `not_local:` |
| `Native/MediaLibrary.swift` | `MediaProvider` (PhotoKit) и `requestPermissions`; общие `fetchAsset`, `videoAsset` |
| `Native/Renditions.swift` | `RenditionMaker`: T1/T2 через `PHImageManager`, оригинал по диапазонам через кеш открытых файлов `Originals` |
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
- `requestMediaPermissions(): Promise<boolean>` — `PHPhotoLibrary.requestAuthorization(for: .readWrite)`; `true` при `.authorized` и `.limited`. Системный диалог показывается только при первом запросе, дальше ответ сразу. Текст диалога — `NSPhotoLibraryUsageDescription` из config plugin модуля (`app.plugin.js`). Объяснение отказа и переход в настройки — на экране (TASK-55).

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
| `make ios-test` | скачать XcodeGen 2.46.0 в `build/bin` (sha256 проверяется), сгенерировать `TestHost/PileupNativeTests.xcodeproj`, `xcodebuild build-for-testing`, затем `test-without-building` дважды: сначала UI-тест `PhotoAccessUITests` (выдаёт хосту доступ к фото, см. раздел 7), потом hosted-тесты `PileupNativeTests` на том же установленном хосте |
| `make ios-prebuild` | `npm ci`, `expo prebuild --platform ios --clean --no-install`, `pod install` |
| `make ios-build` | `xcodebuild` Release под симулятор (`Pileup.xcworkspace`, схема `Pileup`, продукт `Pileup.app`) (JS-бандл внутри, Metro не нужен), только активная архитектура, link map для замеров |
| `make ios-smoke` | `scripts/ios-smoke.sh`: установка и запуск на симуляторе, проверка ядра (раздел 7); печатает идентификатор и отображаемое имя из `Info.plist` |
| `make ios-simulator` | всё подряд, кроме тестов |

Симулятор: `IOS_SIMULATOR` (UDID) или по умолчанию последний iPhone самой новой среды iOS из `simctl list`. Подпись: симуляторные сборки подписываются ad-hoc (`-`), Apple-аккаунт не нужен; сборки под устройство нет (TASK-1).

Тесты идут внутри приложения-хоста, а не как тесты SwiftPM без хоста: у процесса `xctest` на симуляторе нет entitlements, и Keychain отвечает `-34018` (`errSecMissingEntitlement`). Хост — пустое приложение `TestHost`, подписанное ad-hoc, как и само приложение.

## 7. Что доказывает проверка в CI

- `ios-test` (hosted XCTest на симуляторе, 30 тестов, из них 11 — обнаружение и жизненный цикл (раздел 11)): Swift-провайдеры совместимы с протоколами gomobile, xcframework линкуется и ядро работает в iOS-процессе; `Core.start` поднимает ядро, `/health` с токеном — 200, без токена и с чужим — 401; участник pile — `platform = ios` с переданной версией; приглашение создаётся (значит, `NetworkInfo` отдал адрес, который Go признал LAN-адресом); после `Stop` порт закрыт, после нового `Start` — новый токен, старый отвергается, pile и ключ устройства сохранились (seed прочитан из Keychain); `stop` и сразу `start` подряд несколько раз (так `restartCore()` в JS перезапускает ядро при потере связи) — каждый раз новый токен и `/health` 200; `stop` без ядра и повторный `start` ничего не меняют. Отдельно — правила адресов, атрибуты элемента Keychain (`AfterFirstUnlockThisDeviceOnly`, без синхронизации), исключение каталога из бэкапа и сброс seed без каталога.
- Медиатека (`MediaTests`, `OriginalsTests`, `MediaLibraryTests`): тестовые файлы генерируются в самом тесте (JPEG с EXIF-ориентацией 6, PNG, HEIC 2400×1800, видео H.264 с поворотом 90° и HEVC, скрытое фото; двухцветные, чтобы проверить ориентацию по пикселям) и добавляются в медиатеку симулятора через `PHAssetCreationRequest` с заданной `creationDate` — в репозитории медиафайлов нет. Проверяется: поля списка (время, размеры с ориентацией, MIME, имя, размер = длина файла, `codec` `avc1`/`hvc1`, длительность, нет лишних полей; скрытое фото либо отсутствует, либо с `hidden: true`), диапазон-подсказка; T1/T2 — JPEG размера `fit`, без EXIF-ориентации, цвета на нужных сторонах; оригинал по кускам 4 КиБ совпадает с файлом; неизвестный `local_id` — ошибка без `not_local:` быстрее 1 с; через ядро (`Core.start`): выбор диапазона видит файлы, скрытое фото не выбрано, thumbhash появляется (T1 отрисован ядром), `GET …/blob/…/2` отдаёт JPEG 1920×1440. **Доступ к фото.** `simctl privacy … grant photos` на Xcode 26.6 / iOS 26 не помогает: строка `kTCCServicePhotos|com.glumpo.pileup.testhost|2` в `TCC.db` есть, а `authorizationStatus(for: .readWrite)` в хосте — `.notDetermined`. Поэтому первым идёт UI-тест `PhotoAccessUITests` (`UITests/`): запускает `TestHost` с `PILEUP_REQUEST_PHOTOS=1`, хост вызывает `requestAuthorization(.readWrite)` и показывает ответ в метке `photo-status`, тест нажимает в алерте springboard «Allow Full Access» (ищет и по другим подписям) и ждёт статус `3` (`.authorized`). Разрешение остаётся за bundle id хоста, и hosted-тесты после `test-without-building` его видят. Без доступа тесты PhotoKit пропускаются (`XCTSkip`), а не висят. Прогон 95.1 (`8b32806`): `MediaLibraryTests` — 8 из 8 выполнены, 0 пропущено. Кеш `Originals` — на временных файлах: один `open` на параллельные Range, не больше 2 открытых, простой, memory warning, ошибка открытия не запоминается.
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
- Медиатека — разделы 13 и 14.
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

## 13. Медиатека (A49)

**Список (`MediaLibrary.media`).** Без доступа (`.denied`, `.restricted`, `.notDetermined`) — пустой массив и событие `media` в журнале; PhotoKit без разрешения не трогается. Иначе `PHAsset.fetchAssets`: фото и видео, `creationDate` в `[from − 14 ч, to + 14 ч]` (точный фильтр по `taken_at` — в ядре, A17), `includeHiddenAssets` (но на iOS 26 скрытые фото PhotoKit не отдаёт вовсе — ни в списке, ни по `localIdentifier`, проверено тестом; они просто не попадают в выбор); серии — только ключевой кадр (умолчание PhotoKit), общие альбомы не входят. Поля `node.MediaItem`:

| Поле | Откуда |
|---|---|
| `local_id` | `localIdentifier` как есть (формат менять нельзя, A21/A49) |
| `kind` | `video` для `mediaType == .video`, иначе `photo` (живое фото — `photo`) |
| `platform_time` | `creationDate`, мс (§5.4); `file_time` и EXIF не передаются |
| `width`, `height` | `pixelWidth`, `pixelHeight` — уже с ориентацией |
| `duration_ms` | `duration` видео |
| `size_bytes`, `mime`, `original_name` | ресурс оригинала (ниже): `fileSize` (ключ KVC ресурса, иначе 0), MIME по `uniformTypeIdentifier`, исходное имя (`IMG_0001.HEIC`) с расширением ресурса |
| `codec` | FourCC первой видеодорожки (`avc1`, `hvc1`) из `requestAVAsset` без сети; кеш в памяти по (local_id, modified); видео только в iCloud — пусто |
| `modified` | `modificationDate`, мс (ревизия) |
| `screenshot`, `hidden` | `mediaSubtypes.contains(.photoScreenshot)`, `isHidden` |

GPS не передаётся. Стоимость: на каждый файл — `PHAssetResource.assetResources(for:)` (чтение БД медиатеки, без файла); на каждое видео при первом вызове — `requestAVAsset` и разбор заголовка файла, параллельно (`concurrentPerform`), дальше из кеша. На устройстве не замерено (раздел 14).

**Оригинал — текущая версия.** Ресурс `fullSizePhoto`/`fullSizeVideo`, если файл отредактирован, иначе `photo`/`video` (`servedResource`). `OriginalSize` открывает файл и кладёт его в кеш `Originals`, `ReadOriginal` читает `pread` не больше запрошенной длины:

- видео — файл `AVURLAsset.url` из `requestAVAsset` (`.current`, `.highQualityFormat`); если вернулась `AVComposition` (slow-mo) или файл не открылся — запасной путь;
- фото и запасной путь — `PHAssetResourceManager.writeData` ресурса во временный файл; файл удаляется сразу после `open`, дескриптор остаётся;
- открыто не больше 2 оригиналов (вытесняется давно не читанный), закрытие через 60 с простоя (таймер на фоновой очереди) и по `didReceiveMemoryWarning`; всё под одной блокировкой, поэтому параллельные Range одного файла открывают его один раз;
- обработчики PhotoKit приходят на фоновых очередях, вызовы Go ждут их семафором не дольше 15 с, главная очередь не нужна.

**iCloud.** `isNetworkAccessAllowed = false` везде. Если PhotoKit отвечает `PHPhotosError.networkAccessRequired` или `PHImageResultIsInCloudKey`, ошибка — `not_local: <local_id> is only in iCloud, not on this iPhone` (текст `NSError` доходит до Go как `localizedDescription`, поэтому префикс в начале строки; ядро делает из него `ErrNotLocal`, A56). Отказ приходит уже в `OriginalSize`. Неизвестный `local_id` — обычная ошибка без префикса.

**T1/T2 (`Rendition`).** `PHImageManager.requestImage` синхронно, без сети, `.current`, `.exact`, `aspectFit` в квадрат 256 или 1920: сначала `.highQualityFormat`, при отказе — `.fastFormat` (любой локальный дериватив, так T1/T2 строятся и при «Оптимизировать хранилище»). Размер — `fit` по размерам файла, но не больше полученной картинки (`renditionSize`); картинка перерисовывается `UIGraphicsImageRenderer` (scale 1, sRGB, ориентация применяется к пикселям) в JPEG q85. Для видео PhotoKit отдаёт кадр-постер. Оригинал целиком не декодируется: уменьшение делает PhotoKit.

## 14. Медиатека: что не проверено на симуляторе и как проверить на iPhone

Список, T1/T2 (HEIC, JPEG с ориентацией, PNG, постеры H.264 и HEVC), чтение оригинала по диапазонам и путь через ядро проверены на симуляторе (раздел 7). В симуляторе нет iCloud, «Оптимизировать хранилище», отредактированных в «Фото» файлов, slow-mo, Dolby Vision а системный диалог в CI нажимает UI-тест (диалог «Ограниченный доступ» не проверяется). Проверить на устройстве (TASK-20):

1. **iCloud.** Включить «Оптимизировать хранилище», дождаться выгрузки оригиналов (или взять старое видео, у которого в «Фото» есть значок загрузки). Выбрать его в Pileup и показать на ТВ: на пульте «есть только в iCloud», в журнале владельца ответ `OriginalSize` с `not_local:` меньше чем за секунду, трафика из iCloud нет. T1/T2 такого файла строятся, T2 — из локального дериватива (может быть меньше 1920 px).
2. **Отредактированные фото и видео.** Обрезать фото и видео в «Фото»: T1/T2 и оригинал — обрезанные, `size_bytes` списка совпадает с длиной, которую отдаёт `OriginalSize`; после «Вернуть к оригиналу» ревизия меняется и превью — исходные.
3. **Slow-mo и живые фото.** Slow-mo отдаётся исходным файлом через временную копию; живое фото — снимком (`photo`).
4. **Скорость.** Список на 5000 файлов (из них ~300 видео): время первого и повторного `GET /selection`; 4K HEVC на ТВ: время `OriginalSize`, мс на `ReadOriginal` 1 МиБ, память процесса.
5. **Ревизии.** Отметить фото «избранным», добавить в альбом: меняется ли `modificationDate` (а с ним ревизия и повторный рендер T1) без правки содержимого.
6. **Разрешения.** Первый запрос — системный диалог с текстом из `app.plugin.js`; «Ограниченный доступ» — список только из выбранных файлов, `requestMediaPermissions` — `true`; «Не разрешать» — `false`, повторный запрос сразу `false`.
7. **Memory warning** во время показа видео: файлы закрываются, следующий Range открывает заново без ошибки.
