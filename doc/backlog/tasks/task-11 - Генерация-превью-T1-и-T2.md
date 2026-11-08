---
id: TASK-11
title: Генерация превью T1 и T2
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T21:27:00Z'
labels:
  - app
  - media
  - ios
  - android
milestone: m-0
dependencies:
  - TASK-9
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Для сетки нужны миниатюры (T1), для экрана — display-версия (T2). HEIC не открывается на ТВ, поэтому T2 всегда JPEG. Генерацию делает нативный слой (RenditionMaker): он быстрее и сам понимает HEIC и ориентацию. thumbhash нужен, чтобы сетка открывалась мгновенно ещё до загрузки миниатюр.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 T1 — миниатюра 256px, T2 — JPEG до 3840px по длинной стороне с учтённой ориентацией; для видео T2 — постер
- [ ] #2 HEIC с iPhone конвертируется в JPEG
- [ ] #3 thumbhash вычисляется и попадает в AssetMeta
- [ ] #4 После правки фото в галерее телефона меняется revision и генерируются новые превью
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начаты: Go-часть (thumbhash в AssetMeta, публикация обновлений) и Android-генерация T1/T2/T3. iOS — позже.

Android RenditionMaker (2026-11-08): Renditions реализует mobile.RenditionMaker. Фото: API 28+ ImageDecoder (HEIC → JPEG, EXIF-ориентация применяется декодером, setTargetSize, программный аллокатор), API 24–27 BitmapFactory с inSampleSize + ExifInterface и матрица поворота/отражения. T1 — до 256px по длинной стороне, T2 — до 3840px, без увеличения, JPEG качество 85. Видео: T1/T2 — кадр MediaMetadataRetriever.getFrameAtTime(), масштабированный. T3: OriginalSize = statSize дескриптора ContentResolver, ReadOriginal — позиционное чтение FileChannel. На Android 10+ без ACCESS_MEDIA_LOCATION система отдаёт оригинал с вырезанными GPS-тегами — это нам подходит. JVM-тесты масштаба, inSampleSize, таблицы EXIF-ориентаций, границ чтения.
Осталось: проверка на устройстве (HEIC, ориентация, поворот кадра видео), iOS. thumbhash — Go-часть TASK-11 (параллельно). Риски: поворот кадра видео из MediaMetadataRetriever не проверен; на Android 10 statSize редактирующего дескриптора может быть -1; PNG с прозрачностью даёт чёрный фон в JPEG; T3 открывает файл на каждый блок 1 МБ.
<!-- SECTION:NOTES:END -->
