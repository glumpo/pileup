---
id: TASK-18
title: Показ на Apple TV через AirPlay с iPhone-хоста
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - app
  - ios
  - airplay
  - show
milestone: m-0
dependencies:
  - TASK-14
documentation:
  - architecture.md
priority: high
type: spike
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Если в комнате Apple TV, рядом есть iPhone, поэтому AirPlay доступен только с iPhone-хоста (architecture.md, 2.2). Фото показываются через «Повтор экрана» отдельной презентационной сценой, а не зеркалом телефона; видео — через AVPlayer с external playback. Включить повтор экрана программно нельзя, поэтому важна понятная подсказка — это главный риск задачи.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 При включённом «Повторе экрана» на Apple TV показывается полноэкранный слайд, а телефон остаётся пультом
- [ ] #2 Видео с обоих источников проигрывается на Apple TV через AirPlay, включая HEVC; результат записан в задаче
- [ ] #3 Приложение даёт понятную подсказку, как включить «Повтор экрана»
- [ ] #4 Во время показа включён keep-awake и подсказка не блокировать телефон
- [ ] #5 На Android-хосте Apple TV виден в списке экранов (_airplay._tcp) с текстом, что показ на Apple TV доступен только с iPhone
<!-- AC:END -->
