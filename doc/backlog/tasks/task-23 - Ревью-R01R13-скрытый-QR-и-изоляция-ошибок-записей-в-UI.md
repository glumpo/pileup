---
id: TASK-23
title: 'Ревью R01+R13: скрытый QR и изоляция ошибок записей в UI'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-10T04:10:00Z'
updated_date: '2026-11-10T04:10:00Z'
labels:
  - app
  - review
dependencies: []
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: medium
type: bug
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, R01 (P2): приглашение с секретом показывается при открытии экрана pile. R13 (P2): ошибка даты или thumbhash одной записи ломает весь экран.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Приглашение раскрывается только по явному действию и скрывается при уходе с экрана
- [ ] #2 Некорректная дата или thumbhash одной записи показываются локальной заглушкой, остальные записи и навигация работают
- [ ] #3 Каждая ошибка сначала воспроизведена красным тестом
<!-- AC:END -->
