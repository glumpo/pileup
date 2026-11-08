---
id: TASK-11
title: Генерация превью T1 и T2
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T21:00:00Z'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Go-часть (thumbhash):
1. Пакет core/thumbhash: порт эталонного алгоритма ThumbHash (Evan Wallace) для непрозрачного изображения с уменьшением до 100px; тест на эталонных векторах из JS-пакета thumbhash.
2. core/node: фоновый цикл на pile — для своих опубликованных файлов без thumbhash берёт T1 через общий путь blob (T1 сохраняется и не рендерится повторно), декодирует JPEG, считает thumbhash и публикует AssetAdd пачками не больше ThumbhashBatch; публикует только если ревизия не изменилась (под тем же замком, что выбор). Ошибки рендера/декодирования запоминаются в памяти до перезапуска и не блокируют остальные файлы. Прогресс — сами опубликованные AssetMeta, после перезапуска продолжаем с файлов без thumbhash.
3. Повторный выбор сохраняет уже вычисленный thumbhash при той же ревизии; новая ревизия публикуется без него и пересчитывается.
4. Тесты (фейковый RenditionMaker с настоящими JPEG): thumbhash у всех своих файлов виден у участника, пачки ограничены, правка даёт новый thumbhash, сбой рендера не блокирует остальные, перезапуск продолжает работу; обновить тесты TASK-12, считавшие, что T1 не рендерится без запроса.
5. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начаты: Go-часть (thumbhash в AssetMeta, публикация обновлений) и Android-генерация T1/T2/T3. iOS — позже.
<!-- SECTION:NOTES:END -->
