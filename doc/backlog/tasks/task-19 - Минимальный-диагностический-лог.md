---
id: TASK-19
title: Минимальный диагностический лог
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T21:31:00Z'
labels:
  - core
  - app
  - diag
milestone: m-0
dependencies:
  - TASK-3
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Сетевые проблемы между разными телефонами и ТВ сложно отлаживать без журнала событий на устройстве. Лог локальный, не реплицируется; полноценная диагностика — на этапе 4.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Ядро пишет события discovery, подключений, синхронизации, таймаутов и ошибок экранов в кольцевой буфер
- [ ] #2 Лог можно посмотреть в приложении
- [ ] #3 Лог можно экспортировать файлом для баг-репорта
<!-- AC:END -->
