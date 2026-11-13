---
id: TASK-45
title: 'iOS: интерфейс приложения на iPhone'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-11T22:11:00Z'
labels:
  - ios
  - app
  - ui
milestone: m-0
dependencies: []
priority: medium
type: feature
ordinal: 45000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Экраны писались и проверялись только под Android: возврат назад рассчитан на аппаратную кнопку (BackHandler), безопасные области экрана (вырез, индикатор Home) не учитываются, тексты подсказок и разрешений частично платформенные. На iPhone приложение должно быть так же удобно, как на Android.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 С каждого экрана можно вернуться назад без аппаратной кнопки; содержимое не уходит под вырез и индикатор Home
- [ ] #2 Все сценарии (создать pile, войти по QR, выбрать файлы, хронология, просмотр, показ, журнал с экспортом) проходят в тестах приложения с Platform.OS = ios
- [ ] #3 Идентификатор приложения и отображаемое имя заданы для iOS и Android; разрешения камеры и медиатеки имеют понятные тексты
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Тесты приложения под обеими платформами: jest projects с пресетами jest-expo/ios и jest-expo/android (сейчас jest-preset по умолчанию даёт Platform.OS = ios, Android не прогоняется явно).
2. Точечные тесты iOS: возврат назад кнопками в шапке и «✕» на каждом экране без BackHandler; пульт только «Остановить показ»; отступы от безопасных областей.
3. Безопасные области через react-native-safe-area-context (Screen, просмотр, экраны запуска/ошибки), клавиатура — automaticallyAdjustKeyboardInsets.
4. Имя Pileup и идентификатор com.glumpo.pileup для iOS и Android; поправить Makefile, ios-sizes.sh, doc/adr/ios.md.
5. CI: check, android, ios-app зелёные; скриншот до/после; A51 и doc/adr/ios.md.
<!-- SECTION:PLAN:END -->
