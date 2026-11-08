---
id: TASK-8
title: Синхронизация журналов между узлами
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T11:03:00Z'
labels:
  - core
  - network
  - protocol
milestone: m-0
dependencies:
  - TASK-5
  - TASK-7
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Метаданные pile (участники, файлы) должны оказаться у всех участников, как бы и в каком порядке узлы ни встречались. Обмен идёт по version vectors, ответ строго по порядку seq, так что на узлах всегда хранится непрерывный префикс журнала каждого автора (architecture.md, 5.1).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 После соединения двух узлов их состояния сходятся без ручных действий
- [ ] #2 Операции чужого автора, полученные через третий узел, принимаются и проверяются по подписи автора
- [ ] #3 После разрыва и повторного соединения передаются только недостающие операции
- [ ] #4 Перед первой записью после старта узел сверяет свой seq с пирами и догружает собственный журнал, если у пиров он длиннее
<!-- AC:END -->
