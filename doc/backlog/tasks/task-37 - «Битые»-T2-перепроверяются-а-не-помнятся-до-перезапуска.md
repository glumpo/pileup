---
id: TASK-37
title: «Битые» T2 перепроверяются, а не помнятся до перезапуска
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
priority: medium
type: bug
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Файл с ошибкой загрузки T2 (404, отказ владельца, ошибка своего рендера) помечается «битым» до перезапуска ядра (A26, R27) и больше не планируется: временная ошибка рендера становится постоянной, даже если владелец уже исправил файл. «Мелкое» в doc/architecture-review.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 «Битый» файл снова планируется через несколько минут; до этого он не блокирует источник и считается в прогрессе как неудачный
- [ ] #2 Каждое поведение сначала воспроизведено красным тестом
- [ ] #3 doc/adr/cache.md обновлён
<!-- AC:END -->
