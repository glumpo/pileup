---
id: TASK-2
title: Каркас монорепо и CI
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - core
  - app
  - ci
milestone: m-0
dependencies: []
documentation:
  - architecture.md
priority: high
type: task
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Всё дальнейшее строится в одном репозитории: Go-ядро, приложение на React Native (Expo), схема API. Проект открытый под Apache-2.0, и GPL-зависимости недопустимы из-за несовместимости с App Store, поэтому проверка лицензий нужна с первого коммита, а не задним числом.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Репозиторий содержит каталоги core/ (Go-модуль), app/ (Expo-приложение), api/ (схема API)
- [ ] #2 В корне лежит LICENSE с текстом Apache-2.0
- [ ] #3 CI на каждый push запускает go test и сборку ядра
- [ ] #4 CI проверяет лицензии Go- и npm-зависимостей и падает на GPL, AGPL и LGPL
<!-- AC:END -->
