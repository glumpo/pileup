---
id: TASK-40
title: Экран пульта не гаснет во время показа
status: To Do
assignee:
  - '@claude'
created_date: '2026-11-10T22:58:00Z'
labels:
  - app
dependencies: []
documentation:
  - architecture-review.md
priority: high
type: bug
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Хост — это и пульт, и сервер медиа для ТВ, и клиент источников. Во время видео телефон никто не трогает, экран гаснет, и Android через некоторое время (light Doze) отрезает приложению сеть: пропадает heartbeat Cast, ТВ не может забрать следующий файл. keep-awake для Android нет ни в коде, ни в бэклоге (есть только для iPhone в TASK-18). Н7 в doc/architecture-review.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Пока открыт экран пульта, экран телефона не гаснет; после выхода из показа запрет снимается
- [ ] #2 Новая зависимость проходит проверку лицензий
- [ ] #3 Каждое поведение сначала воспроизведено красным тестом
<!-- AC:END -->
