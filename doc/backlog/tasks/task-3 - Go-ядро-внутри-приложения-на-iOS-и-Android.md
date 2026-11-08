---
id: TASK-3
title: Go-ядро внутри приложения на iOS и Android
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:59:00Z'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Android-часть (iOS отложена: нет Mac).
1. core/mobile: gomobile-фасад Start() → порт и токен через геттеры, Stop(); сервер core/httpapi на 127.0.0.1 со случайным портом и токеном → проверка: go test (health 200 с токеном / 401 без, после Stop порт закрыт, повторный Start работает).
2. Тулчейн: JDK 17, Android SDK (cmdline-tools, platform/build-tools/NDK — версии из prebuild-проекта Expo 58 / RN 0.88), gomobile/gobind → проверка: gomobile bind -target=android собирает AAR.
3. Makefile: mobile-android (AAR в app/modules/core/android/libs, gitignored) и android-debug (AAR → expo prebuild → gradlew assembleDebug); make check не требует Android.
4. app/modules/core: локальный Expo-модуль, сгенерированный create-expo-module --local; Kotlin-реализация start/stop/port/token поверх AAR; iOS — как сгенерировано, без ядра.
5. Config plugin: cleartext HTTP только к 127.0.0.1/localhost через network_security_config; android/ и ios/ не коммитятся (CNG).
6. Экран App.tsx: при запуске стартует ядро, fetch GET /health с токеном, показ результата.
7. Замеры: время сборки и размер debug APK шаблона без модуля и с ядром; проверка libgojni.so в APK.
8. Финальные проверки: gofmt, go vet, go test, make check, tsc --noEmit, полная сборка APK.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Android-часть и Go-фасад. iOS-часть ждёт Mac/раннер и аккаунт Apple (TASK-1).
<!-- SECTION:NOTES:END -->
