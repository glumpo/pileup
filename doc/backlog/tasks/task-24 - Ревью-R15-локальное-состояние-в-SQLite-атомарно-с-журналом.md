---
id: TASK-24
title: 'Ревью R15: локальное состояние в SQLite атомарно с журналом'
status: To Do
assignee: []
created_date: '2026-11-10T04:10:00Z'
labels:
  - core
  - review
dependencies:
  - TASK-21
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: high
type: bug
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, R15 (P1): selection.json (выбор и соответствие asset_id → local_id) пишется отдельно от AssetAdd/AssetRemove; сбой между шагами оставляет расхождение. Нужна инвентаризация состояния вне SQLite и обоснование места хранения.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Выбор, ручные решения и соответствие локальным файлам хранятся в SQLite и меняются одной транзакцией вместе с AssetAdd/AssetRemove
- [ ] #2 Существующие selection.json переносятся при открытии, параллельной записи в старый файл нет
- [ ] #3 Инвентаризация состояния вне SQLite с обоснованием места хранения записана в architecture.md
- [ ] #4 Сбой до commit, между шагами и после commit проверен красным тестом до исправления
<!-- AC:END -->
