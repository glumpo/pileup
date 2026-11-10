---
id: TASK-40
title: Экран пульта не гаснет во время показа
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T22:58:00Z'
updated_date: '2026-11-10T23:53:00Z'
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
- [x] #1 Пока открыт экран пульта, экран телефона не гаснет; после выхода из показа запрет снимается
- [x] #2 Новая зависимость проходит проверку лицензий
- [x] #3 Каждое поведение сначала воспроизведено красным тестом
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Зависимость expo-keep-awake в версии из bundledNativeModules.json Expo SDK 58 (~58.0.3) через npx expo install; проверить make licenses-npm.
2. Красный тест в app/test/show.test.tsx: jest.mock('expo-keep-awake') — подделка модуля, которая помнит удерживаемые теги (хук и императивные функции); экран не держится до показа, держится, пока открыт пульт, отпускается после «Остановить показ».
3. Remote.tsx: useKeepAwake() в начале компонента — запрет сна живёт ровно столько, сколько смонтирован пульт (и для пустого показа).
4. Проверки: tsc, jest, make licenses-npm. Документы: ADR по приложению нет; заметка в задаче.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красный тест (2a81f29): show.test.tsx «the phone screen is kept on while the remote is open and released after the show» — expo-keep-awake подменён jest.mock-подделкой, которая помнит удерживаемые теги (useKeepAwake и activateKeepAwakeAsync/deactivateKeepAwake). Падение: после старта показа на «Гостиной» «expect(received).toBe(expected) — Expected: true, Received: false» (show.test.tsx:423).

Реализация (a3faa3a): зависимость expo-keep-awake ~58.0.3 — версия из bundledNativeModules.json expo 58.0.23 (npx expo install); раньше та же версия лежала вложенной в expo, теперь поднята в корень lock-файла. Remote.tsx вызывает useKeepAwake() рядом с остальными хуками, до ветки «В pile не осталось файлов», поэтому запрет сна живёт ровно пока смонтирован пульт; после «Остановить показ» пульт размонтируется и хук снимает запрет. Конфиг-плагина и разрешений Android не нужно (FLAG_KEEP_SCREEN_ON окна); модуль подключается автолинковкой при prebuild.

Проверка: npx tsc --noEmit — без ошибок; npx jest --ci — 9 наборов, 51 тест, все прошли (в том числе на свежем node_modules после npm ci); make licenses-npm — exit 0, expo-keep-awake@58.0.3 MIT, npm ci lock-файл не меняет.

Ограничения:
- Проверено только в jest; на устройстве (видео 5 минут без касаний, Android) не проверялось — нужен прогон на телефоне.
- Держится только экран пульта: если пользователь сам заблокирует телефон или уведёт приложение в фон, Doze по-прежнему может отрезать сеть; foreground service — только по замерам TASK-20 (Н7).
- Источники (не хосты) по-прежнему могут засыпать: баннер «не блокируйте экран, идёт раздача» (§6.3) не сделан, это не входит в задачу.
- iPhone-хост: тот же хук работает и на iOS (isIdleTimerDisabled), но показ с iPhone не реализован (TASK-18).

Решение для architecture.md: Пока открыт экран пульта (маршрут show, включая показ без файлов), приложение держит экран телефона включённым через expo-keep-awake (useKeepAwake, версия из набора Expo SDK); после «Остановить показ» запрет снимается. Foreground service не используется. Дополняет A25 | Хост — и пульт, и сервер медиа для ТВ, и клиент источников; при погасшем экране Android в light Doze отрезает сеть, и показ останавливается без видимой причины (Н7 в architecture-review.md). Одна строка закрывает основной случай, разрешений не требует | TASK-40
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Пульт (Remote.tsx) вызывает useKeepAwake из expo-keep-awake ~58.0.3 (версия Expo SDK 58): пока открыт экран пульта, экран телефона не гаснет, после «Остановить показ» запрет снимается — Android в light Doze больше не отрезает хосту сеть из-за погасшего экрана. Красный тест в show.test.tsx с jest-подделкой модуля; tsc, jest 51/51, make licenses-npm (MIT) — ok. На устройстве не проверялось; источники и ручная блокировка экрана не покрыты (foreground service — по замерам TASK-20).
<!-- SECTION:FINAL_SUMMARY:END -->
