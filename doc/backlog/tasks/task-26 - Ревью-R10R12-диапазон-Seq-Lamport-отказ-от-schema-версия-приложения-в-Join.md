---
id: TASK-26
title: 'Ревью R10+R12: диапазон Seq/Lamport, отказ от schema, версия приложения в Join'
status: To Do
assignee: []
created_date: '2026-11-10T04:10:00Z'
labels:
  - core
  - protocol
  - review
dependencies:
  - TASK-22
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: medium
type: task
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, R10 (P2): uint64 Seq/Lamport против знакового INTEGER SQLite, переполнение L+1. R12 (P2, согласовано): убрать schema из операций, развивать протокол новыми типами, добавить AppVersion в Join. Формат меняем до релиза, пока это дёшево.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Seq и Lamport вне согласованного диапазона отклоняются до сохранения; следующая запись не переполняется
- [ ] #2 Операции создаются без schema; неизвестный тип после Join сохраняется и пересылается, известные операции после него применяются
- [ ] #3 AppVersion из Join доступна в данных участника; Join без версии допустим
- [ ] #4 Изменения сначала зафиксированы красными тестами
<!-- AC:END -->
