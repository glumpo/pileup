---
id: TASK-10
title: Время съёмки taken_at
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:34:00Z'
labels:
  - core
  - media
milestone: m-0
dependencies:
  - TASK-9
documentation:
  - architecture.md
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Хронология строится только по времени съёмки. Фото хранят локальное время без пояса, видео — UTC, поэтому сравнение сырых значений систематически сдвигает видео на величину пояса. Алгоритм выбора taken_at описан в architecture.md, 5.4; его нужно реализовать детерминированно и подтвердить на реальных устройствах — это один из рисков этапа.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Функция в Go выбирает taken_at по правилам 1–6 из раздела 5.4 и записывает taken_at_source; каждое правило покрыто табличным тестом
- [ ] #2 Пояс текущего устройства в вычислении taken_at не используется
- [ ] #3 На реальных iPhone и Android фото и видео одного телефона стоят в правильном порядке
- [ ] #4 Фото и видео двух телефонов, снятые вперемешку, стоят в правильном порядке; результаты проверки записаны в задаче
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата только Go-часть (функция выбора taken_at и табличные тесты, AC #1–#2). Проверка на реальных устройствах (AC #3–#4) ждёт TASK-9 и устройств.
<!-- SECTION:NOTES:END -->
