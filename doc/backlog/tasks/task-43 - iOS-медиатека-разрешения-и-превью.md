---
id: TASK-43
title: 'iOS: медиатека, разрешения и превью'
status: To Do
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
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
- [ ] #1 MediaProvider отдаёт фото и видео из PhotoKit в формате node.MediaItem: local_id, kind, platform_time, размеры с учётом ориентации, длительность, размер, mime, имя, modified, флаги screenshot и hidden; GPS не передаётся
- [ ] #2 requestMediaPermissions запрашивает доступ к медиатеке; ограниченный доступ считается доступом; текст запроса задан в Info.plist через config plugin
- [ ] #3 RenditionMaker отдаёт T1 (256 px) и T2 (до 1920 px) в JPEG с правильной ориентацией для HEIC/JPEG/PNG и постер для видео; размеры считает та же логика, что на Android
- [ ] #4 Оригинал читается по диапазонам (OriginalSize, ReadOriginal) без загрузки файла целиком в память
- [ ] #5 Файл, оригинал которого только в iCloud, даёт понятную ошибку, а не падение или зависание; T1/T2 строятся из локальных деривативов
- [ ] #6 Чистая логика покрыта unit-тестами; на симуляторе в CI интеграционный тест получает список медиа и рендерит T1/T2 из тестовых фото симулятора
<!-- AC:END -->
