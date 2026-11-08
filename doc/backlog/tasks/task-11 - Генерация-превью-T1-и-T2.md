---
id: TASK-11
title: Генерация превью T1 и T2
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
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
