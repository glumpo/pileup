---
id: TASK-3
title: Go-ядро внутри приложения на iOS и Android
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - core
  - app
  - ios
  - android
milestone: m-0
dependencies:
  - TASK-1
  - TASK-2
documentation:
  - architecture.md
priority: high
type: spike
ordinal: 3000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ключевой технический риск выбранного стека: Go-ядро через gomobile должно встраиваться в Expo-приложение (prebuild + config plugin) на обеих платформах, а UI должен общаться с ядром по localhost HTTP и WebSocket с токеном. Это тот же API, что потом получат веб-плеер и десктоп. Если связка не заработает, стек придётся пересматривать, поэтому проверяем как можно раньше.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 На iPhone и Android ядро стартует вместе с приложением и возвращает порт и токен
- [ ] #2 UI получает ответ ядра по 127.0.0.1 на обеих платформах; запрос без токена отклоняется
- [ ] #3 Ядро корректно останавливается при выходе из приложения и запускается снова
- [ ] #4 В задаче записаны время сборки и размер приложения с ядром и без него на обеих платформах
<!-- AC:END -->
