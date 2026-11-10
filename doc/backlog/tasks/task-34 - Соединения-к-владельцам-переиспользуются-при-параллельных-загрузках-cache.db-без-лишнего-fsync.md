---
id: TASK-34
title: >-
  Соединения к владельцам переиспользуются при параллельных загрузках, cache.db
  без лишнего fsync
status: To Do
assignee:
  - '@claude'
created_date: '2026-11-10T22:58:00Z'
labels:
  - core
dependencies: []
documentation:
  - architecture-review.md
  - adr/cache.md
priority: low
type: enhancement
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Транспорт загрузок у владельцев использует MaxIdleConnsPerHost по умолчанию (2), а параллельных запросов к одному владельцу 5–10, поэтому лишние TLS-рукопожатия повторяются. cache.db открыт с synchronous=FULL: на каждую публикацию fsync WAL под общим мьютексом кеша, хотя кеш сверяется с диском при открытии. «Мелкое» в doc/architecture-review.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 После серии параллельных загрузок у одного владельца следующие загрузки той же параллельности не открывают новых соединений
- [ ] #2 cache.db открывается с synchronous=NORMAL, сверка с диском при открытии восстанавливает согласованность после обрыва
- [ ] #3 doc/adr/cache.md обновлён
<!-- AC:END -->
