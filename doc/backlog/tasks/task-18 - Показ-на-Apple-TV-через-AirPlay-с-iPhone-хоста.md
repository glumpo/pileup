---
id: TASK-18
title: Показ на Apple TV через AirPlay с iPhone-хоста
status: Done
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-13T13:32:00Z'
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
- [x] #4 Во время показа включён keep-awake и подсказка не блокировать телефон
- [x] #5 На Android-хосте Apple TV виден в списке экранов (_airplay._tcp) с текстом, что показ на Apple TV доступен только с iPhone
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Ядро (сначала красные тесты): AirPlay-сессия на iPhone-хосте открывает медиапорт показа на локальном адресе маршрута к Apple TV; в состоянии показа slide {image, video, around} с URL этого порта; POST /show/player — состояние натив-плеера (buffering/playing/ended/failed) → Playing/Buffering/Stalled/video_unavailable; продолжение прерванного AirPlay-показа по записи A63; фильтр HomePod/AirPort по TXT model.
2. Приложение (сначала красные jest-тесты): подсказка «Повтор экрана» на выборе экрана и на пульте по признаку внешнего экрана из натива; на пульте push слайда в натив после каждого ответа ядра, события плеера → ядро; подсказка не блокировать iPhone.
3. Натив Swift: Presentation (кеш и предекодирование ±1, AVPlayer с external playback), сцена внешнего дисплея (UIWindowSceneSessionRoleExternalDisplayNonInteractive) через app.plugin.js, события в JS; unit-тесты логики и интеграционный тест на симуляторе: контроллер грузит слайд с медиапорта настоящего ядра по LAN-адресу.
4. CI: проверка манифеста сцен в ios-prebuild; все три задания зелёные.
5. Документация: doc/adr/ios.md, A52, сценарий проверки на iPhone + Apple TV.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Android-сторона AC #5 в ядре (TASK-7): узел ищет _airplay._tcp, GET /screens отдаёт Apple TV на не-iOS хосте с available=false и reason «Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника.» (имя экрана — отдельным полем name, как в architecture.md 2.2); на ios — available=true. Показ этого списка в UI — TASK-14. Риск: _airplay._tcp объявляют и HomePod/Mac, фильтра пока нет.

Сделано (A52, doc/adr/ios.md §15): медиапорт показа для AirPlay на локальном адресе маршрута к Apple TV, slide {image, video, around} в состоянии показа, POST /show/player (buffering/playing/ended/failed), продолжение AirPlay-показа по записи A63, HomePod/AirPort (TXT model) не экраны. Приложение: подсказка «Повтор экрана» на выборе экрана и пульте по признаку внешнего экрана, present(slide) из каждого ответа ядра, события плеера в ядро, «Не блокируйте iPhone…». Натив: Native/Presentation.swift — сцена UIWindowSceneSessionRoleExternalDisplayNonInteractive (PileupPresentationSceneDelegate через app.plugin.js), предекодирование соседних слайдов, AVPlayer с external playback.
Доказано: Go core/show/airplay_test.go, core/httpapi (TestAppleTVSlideAndPlayerStateThroughAPI, TestSpeakersWithAirPlayAreNotScreens, TestAndroidHostListsAppleTVWithExplanation), jest test/airplay.test.tsx и show.test.tsx (Apple TV с причиной на Android, keep-awake пульта), ios-native SlideTests + PresentationTests (контроллер показывает слайд с медиапорта настоящего ядра на LAN-адресе), ios-app: манифест сцен в Info.plist и класс делегата в бинарнике.
AC #1–#3 не отмечены: нужна проверка на iPhone + Apple TV по сценарию doc/adr/ios.md §15 (сцена вместо зеркала, видео HEVC/H.264 через external playback, понятность подсказки). Риски: UIApplicationSupportsMultipleScenes=false и правило iOS 27 о scene accessory (SDK 27 в CI нет).

Лид: слито в master. Критерии 1–3 не отмечены осознанно: презентационная сцена, AirPlay-видео и понятность подсказки проверяются только на iPhone с Apple TV — пошаговый сценарий из 12 шагов в doc/adr/ios.md §15, проверка в TASK-20. Два главных риска (создаётся ли сцена внешнего экрана при UIApplicationSupportsMultipleScenes=false; правило iOS 27 про registerSceneAccessory) описаны там же.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Показ на Apple TV с iPhone-хоста реализован по документации Apple и покрыт всем, что проверяемо без устройств (Go, jest, XCTest на симуляторе с настоящим ядром, сборка с манифестом сцен в CI). Фото — сцена внешнего дисплея со слайдом, который JS пульта передаёт нативу из каждого ответа ядра (соседние слайды декодируются заранее); видео — AVPlayer с external playback по LAN-URL медиапорта показа с токеном; состояние плеера идёт в ядро через POST /show/player. Подсказка «Повтор экрана» и «не блокируйте iPhone» — на выборе экрана и пульте. Не проверено: создание сцены на устройстве (UIApplicationSupportsMultipleScenes=false, iOS 27 scene accessory), external playback HEVC/H.264, понятность подсказки — сценарий в doc/adr/ios.md §15 (A52).
<!-- SECTION:FINAL_SUMMARY:END -->
