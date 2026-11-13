---
id: TASK-43
title: 'iOS: медиатека, разрешения и превью'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-13T12:59:00Z'
labels:
  - ios
  - app
  - media
milestone: m-0
dependencies: []
priority: high
type: feature
ordinal: 43000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
iPhone должен быть полноценным источником, как Android (TASK-9, TASK-10, TASK-11): отдавать список медиа из PhotoKit, времена съёмки для алгоритма 5.4, превью T1/T2 (HEIC → JPEG, ориентация, постер видео) и оригинал по диапазонам для стриминга. Особенности iOS из architecture.md §3.1: ограниченный доступ к медиатеке, iCloud «Оптимизировать хранилище» (оригинала может не быть на устройстве), скрытый альбом и скриншоты исключаются по умолчанию.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 MediaProvider отдаёт фото и видео из PhotoKit в формате node.MediaItem: local_id, kind, platform_time, размеры с учётом ориентации, длительность, размер, mime, имя, modified, флаги screenshot и hidden; GPS не передаётся
- [x] #2 requestMediaPermissions запрашивает доступ к медиатеке; ограниченный доступ считается доступом; текст запроса задан в Info.plist через config plugin
- [x] #3 RenditionMaker отдаёт T1 (256 px) и T2 (до 1920 px) в JPEG с правильной ориентацией для HEIC/JPEG/PNG и постер для видео; размеры считает та же логика, что на Android
- [x] #4 Оригинал читается по диапазонам (OriginalSize, ReadOriginal) без загрузки файла целиком в память
- [ ] #5 Файл, оригинал которого только в iCloud, даёт понятную ошибку, а не падение или зависание; T1/T2 строятся из локальных деривативов
- [x] #6 Чистая логика покрыта unit-тестами; на симуляторе в CI интеграционный тест получает список медиа и рендерит T1/T2 из тестовых фото симулятора
- [ ] #7 Ошибки медиатеки и рендера попадают в журнал диагностики; оригинал, которого нет на устройстве, отвечает ошибкой с префиксом not_local: уже в OriginalSize меньше чем за секунду (контракт A56)
- [x] #8 Для видео заполняется codec (hvc1, avc1); при отказе в доступе к медиатеке пользователь получает объяснение и путь в настройки
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Чистая логика в Native/Media.swift: MediaRecord → JSON node.MediaItem, интервал запроса ±14 ч, выбор ресурса текущей версии, имя/MIME, fit/tier/readLength, разрешения, текст not_local; unit-тесты по образцу Android (MediaTest, ScaleTest, PermissionsTest).
2. MediaLibrary.swift: PHAsset fetch (с скрытыми), local_id = localIdentifier, размеры/время/флаги из PHAsset, размер/имя/MIME из PHAssetResource, codec из дорожки видео с кешем по (local_id, modified); requestPermissions через PHPhotoLibrary.requestAuthorization(.readWrite), .limited = доступ.
3. Renditions.swift: T1/T2 через PHImageManager (без сети, сначала highQuality, затем локальный дериватив), перерисовка с ориентацией в JPEG q85; оригинал: видео — файл AVURLAsset, иначе writeData во временный файл; кеш 2 открытых файлов с закрытием через 60 с и по memory warning; not_local: за <1 с.
4. app.plugin.js — NSPhotoLibraryUsageDescription; PileupCoreModule — только requestMediaPermissions; Core.swift — строка провайдеров.
5. Интеграционные тесты на симуляторе: тестовые медиа генерируются в тесте (ImageIO/AVAssetWriter) и добавляются в медиатеку; Makefile — simctl privacy grant photos для TestHost.
6. doc/adr/ios.md, A49; CI зелёный; merge origin/master.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Лид, 2026-11-11: добавлены требования из ревью — doc/review-2026-11-11/video.md, раздел «Вход для TASK-43» (8 требований к OriginalSize/ReadOriginal), находки V3, D7; контракт not_local — doc/adr/cache.md (TASK-50, A56); канал ошибок натива — mobile.Report (TASK-54, A60, doc/adr/diag.md).

Реализация: Native/Media.swift (чистая логика), MediaLibrary.swift, Renditions.swift (Originals — кеш 2 открытых файлов, 60 с простоя, memory warning), NSPhotoLibraryUsageDescription в app.plugin.js, проверка ключа в ios-smoke. local_id = PHAsset.localIdentifier (A49).
CI: MediaTests (19) и OriginalsTests (7) зелёные на симуляторе. MediaLibraryTests (интеграция PhotoKit, 8) на раннере пропускаются XCTSkip: simctl privacy grant photos пишет kTCCServicePhotos|com.glumpo.pileup.testhost|2 в TCC.db (проверено sqlite3 до и после установки хоста), но authorizationStatus(.readWrite) = .notDetermined, requestAuthorization ждёт системного диалога (Xcode 26.6, iOS 26). Тесты написаны, компилируются, но не выполнены.
Риски: modificationDate меняется не только при правке (избранное и т. п.) — ревизия и T1 перерисовываются; fileSize ресурса — через KVC (непубличный ключ, с проверкой responds); codec — requestAVAsset на каждое видео при первом списке, кеш в памяти.

Лид: слито в master, задача остаётся In Progress. Код PhotoKit собран и покрыт unit-тестами чистой логики, но 7 интеграционных тестов MediaLibraryTests на раннере пропускаются (нет доступа к медиатеке у хоста тестов) — то есть сам код работы с PhotoKit ни разу не выполнялся. Следующий шаг: выдать доступ на симуляторе через UI-тест, нажимающий «Разрешить» в системном диалоге, чтобы эти тесты реально шли в CI.

Продолжение (ветка ios/media-tests): доступ к фото на раннере выдаёт UI-тест PhotoAccessUITests — нажимает «Allow Full Access» в алерте springboard (simctl privacy на Xcode 26.6/iOS 26 не действует). Прогон 95.1 (8b32806): MediaLibraryTests 8 из 8 выполнены, 0 пропущено; всего 64 теста, 0 ошибок. Найдено: PhotoKit на iOS 26 не отдаёт скрытые фото вовсе (ни fetchAssets с includeHiddenAssets, ни по localIdentifier) — код не менялся (скрытые и так не должны выбираться), тест принимает оба исхода, A49 и ios.md уточнены. Остальной код PhotoKit ошибок не показал: размеры с ориентацией, fileSize ресурса = длина файла, codec avc1/hvc1, T1/T2 с ориентацией, чтение оригинала по 4 КиБ совпадает с файлом, через ядро thumbhash и T2 1920×1440. Не отмечены #5 и #7: iCloud на симуляторе нет (not_local и деривативы — только unit-тест и устройство). #8: codec проверен тестом, объяснение и «Открыть настройки» — SelectionScreen из TASK-55.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
iPhone отдаёт список медиа из PhotoKit в формате node.MediaItem (local_id = localIdentifier, время creationDate, размеры с ориентацией, MIME/размер/имя текущей версии, codec avc1/hvc1, hidden/screenshot), T1/T2 через PHImageManager без сети с локальными деривативами (JPEG q85, ориентация в пикселях) и оригинал по диапазонам pread из файла AVURLAsset или временной копии ресурса с кешем открытых файлов; iCloud-only даёт not_local: уже в OriginalSize. requestMediaPermissions — .readWrite, .limited = доступ; NSPhotoLibraryUsageDescription через config plugin. Unit-тесты чистой логики и кеша оригиналов зелёные в CI; интеграционные тесты PhotoKit на раннере пропускаются (симулятор не даёт хосту доступ к фото несмотря на simctl privacy) — нужно прогнать на Mac/iPhone; список непроверенного — doc/adr/ios.md §14. Решение A49.
<!-- SECTION:FINAL_SUMMARY:END -->
