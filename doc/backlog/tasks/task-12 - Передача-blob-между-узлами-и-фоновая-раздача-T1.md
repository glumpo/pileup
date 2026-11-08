---
id: TASK-12
title: Передача blob между узлами и фоновая раздача T1
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:39:00Z'
labels:
  - core
  - network
milestone: m-0
dependencies:
  - TASK-8
  - TASK-11
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Источник только анонсирует свои файлы и отдаёт их по запросу; что и когда загружать, решает потребитель (architecture.md, 6.2). Миниатюры нужны у всех участников, чтобы общая сетка работала без задержек.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Blob доступен по /blob/{asset_id}/{revision}/{tier} с Cache-Control immutable и ETag по хешу содержимого
- [ ] #2 Запросы с Range поддерживаются, в том числе для видео-оригиналов
- [ ] #3 Каждый участник фоном загружает T1 всех файлов pile
- [ ] #4 Источник никогда не отправляет данные по своей инициативе
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: реестр превью, отдача blob по /blob/{asset}/{rev}/{tier} с Range поверх peer, фоновая загрузка T1 потребителем. Нативная генерация превью (TASK-11) — через интерфейс.
<!-- SECTION:NOTES:END -->
