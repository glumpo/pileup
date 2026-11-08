---
id: TASK-20
title: Итоговая проверка этапа 1 на реальных устройствах
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - qa
milestone: m-0
dependencies:
  - TASK-13
  - TASK-15
  - TASK-16
  - TASK-17
  - TASK-18
  - TASK-19
documentation:
  - architecture.md
priority: high
type: spike
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Этап считается завершённым, когда сквозной сценарий работает на реальных устройствах: Android и iPhone собирают общий pile, один из них становится хостом и показывает хронологию на Apple TV или Google TV. Итоги должны вернуться в архитектурный документ, чтобы следующие этапы опирались на проверенные факты.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Пройдены все критерии готовности этапа 1 из architecture.md, раздел 10, на устройствах из TASK-1
- [ ] #2 По ~300 файлов с каждого телефона, включая видео, дают одинаковую и правильную хронологию на обоих телефонах
- [ ] #3 Записаны время сборки, размер приложения и вклад Go-рантайма
- [ ] #4 Результаты проверки рисков этапа (AirPlay, Cast по http, HEVC, iPhone-хост, taken_at) внесены в architecture.md
<!-- AC:END -->
