---
id: TASK-45
title: 'iOS: интерфейс приложения на iPhone'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-13T07:42:00Z'
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
- [x] #1 С каждого экрана можно вернуться назад без аппаратной кнопки; содержимое не уходит под вырез и индикатор Home
- [x] #2 Все сценарии (создать pile, войти по QR, выбрать файлы, хронология, просмотр, показ, журнал с экспортом) проходят в тестах приложения с Platform.OS = ios
- [x] #3 Идентификатор приложения и отображаемое имя заданы для iOS и Android; разрешения камеры и медиатеки имеют понятные тексты
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Тесты приложения под обеими платформами: jest projects с пресетами jest-expo/ios и jest-expo/android (сейчас jest-preset по умолчанию даёт Platform.OS = ios, Android не прогоняется явно).
2. Точечные тесты iOS: возврат назад кнопками в шапке и «✕» на каждом экране без BackHandler; пульт только «Остановить показ»; отступы от безопасных областей.
3. Безопасные области через react-native-safe-area-context (Screen, просмотр, экраны запуска/ошибки), клавиатура — automaticallyAdjustKeyboardInsets.
4. Имя Pileup и идентификатор com.glumpo.pileup для iOS и Android; поправить Makefile, ios-sizes.sh, doc/adr/ios.md.
5. CI: check, android, ios-app зелёные; скриншот до/после; A51 и doc/adr/ios.md.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Лид: довёл задачу после агента. На ccf0887 все четыре задания CI зелёные (прогон 74.1 в ci-logs). Скриншот дымового запуска: заголовок «Pileup» стоит ниже Dynamic Island (до правки упирался в него). В логе ios-smoke: bundle com.glumpo.pileup, display name Pileup. Текст запроса доступа к медиатеке добавляет TASK-43. На слитом master: tsc чистый, jest 304/304 в двух проектах (iOS и Android).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Экраны учитывают безопасные области через общий Safe (react-native-safe-area-context, MIT); с каждого экрана можно вернуться кнопкой на экране, с пульта — только «Остановить показ» (A25); все тесты приложения идут на iOS и Android (два проекта jest); приложение называется Pileup, идентификатор com.glumpo.pileup на обеих платформах (A51). Не проверено без iPhone: реальные вырезы разных моделей, клавиатура, VoiceOver, системное окно «Поделиться» — см. doc/adr/ios.md.
<!-- SECTION:FINAL_SUMMARY:END -->
