---
id: TASK-3
title: Go-ядро внутри приложения на iOS и Android
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:22:00Z'
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

## Android: ход работы (2026-11-08)

Сделано:
- `core/mobile` — gomobile-фасад: `Start() error`, `Stop() error`, `Port() int`, `Token() string`. Сервер `core/httpapi` на `127.0.0.1:0`, токен `crypto/rand.Text()` (128 бит), новый на каждый `Start`. Повторный `Start` при работающем ядре ничего не меняет, `Stop` без `Start` — no-op. Тесты: 200 с токеном / 401 без, после `Stop` порт закрыт, повторный `Start` работает с новым токеном, старый отвергается.
- `app/modules/core` — локальный Expo-модуль (`create-expo-module --local`, платформы apple+android). Android (Kotlin): `start()`, `stop()` (async), `port()`, `token()` (sync) поверх AAR; ядро останавливается в `OnActivityDestroys` и `OnDestroy`. iOS оставлен как сгенерирован (hello/setValueAsync), ядро не подключено.
- `app/modules/core/app.plugin.js` — config plugin: `res/xml/network_security_config.xml` (cleartext запрещён, разрешён только к 127.0.0.1 и localhost) и `android:networkSecurityConfig` в манифесте. `app/android`, `app/ios` не коммитятся.
- `App.tsx`: при запуске `start()`, `GET /health` с токеном и без, на экране статусы.
- Makefile: `make mobile-android` (gomobile bind → `app/modules/core/android/libs/pileup-core.aar`, gitignored), `make android-debug` (AAR → npm ci → `expo prebuild --clean` → `gradlew :app:assembleDebug`). `make check` Android-тулчейн не требует.

Тулчейн (установлен на этой машине):
- OpenJDK 17.0.20.1 (apt `openjdk-17-jdk-headless`); JDK 17 требуют RN/Expo (`jvmToolchain(17)`).
- Android SDK в `/opt/android-sdk`: cmdline-tools 23.0 (commandlinetools-linux-16111833), `platforms;android-37.0`, `build-tools;37.0.0`, `ndk;27.1.12297006`, platform-tools. Версии взяты из prebuild-проекта Expo 58 / RN 0.88: compileSdk 37, targetSdk 36, minSdk 24, buildTools 37.0.0, NDK 27.1.12297006, AGP 9.2.1, Gradle 9.4.1, Kotlin 2.2.10.
- gomobile/gobind `golang.org/x/mobile v0.0.0-20261011034528-197770444867` — директива `tool` в `core/go.mod`; Makefile ставит их в `build/bin` через `go install tool`.
- Для Gradle добавлен swap-файл 3 ГБ (`/swapfile-gradle`, не в fstab): на 3,9 ГБ RAM сборка доходила до ~4 ГБ swap.

Воспроизведение: `export PATH=/usr/local/go/bin:$PATH ANDROID_HOME=/opt/android-sdk; make android-debug`. APK: `app/android/app/build/outputs/apk/debug/app-debug.apk`.

Замеры Android (4 CPU, машина делится с другим агентом — время шумное ±30%):
- Базовая линия — шаблон Expo без модуля (копия app до TASK-3, prebuild).
- Debug APK (4 ABI, без JS-бандла): без ядра 143 582 509 байт, с ядром 168 569 127 байт (+24,99 МБ; `libgojni.so` по ABI: arm64 6 048 072, armeabi-v7a 6 151 128, x86 6 190 976, x86_64 6 549 832).
- Release APK, только arm64-v8a (R8, JS-бандл внутри): без ядра 20 901 888 байт, с ядром 26 966 271 байт (+6,06 МБ ≈ `libgojni.so`). R8 сохраняет классы `go.*`/`mobile.*` по proguard.txt из AAR.
- Время Gradle, clean build с прогретым кешем зависимостей и новым демоном: debug без ядра 360 с, с ядром 340 с; release arm64 без ядра 521 с, с ядром 351 с — разница в пределах шума, модуль почти не добавляет времени Gradle.
- `gomobile bind` (4 ABI): 75 с с пустым GOCACHE, 12 с повторно. Полный `make android-debug` первый раз (npm ci + prebuild + сборка): 14 мин 49 с. Самая первая сборка с загрузкой зависимостей Gradle: ~23 мин.

Не проверено (нет устройства/эмулятора): AC2 (ответ ядра на устройстве, отказ без токена), AC3 на устройстве (остановка при выходе и повторный старт). iOS — ждёт Mac.

Проверка владельцем: go test, make check (без ANDROID_HOME), tsc проходят; debug APK собран. Решение A13 записано.
Осталось: проверка на реальном Android-устройстве (AC #1–#3), вся iOS-часть (Swift-модуль, gomobile bind -target=ios, podspec vendored_frameworks, Makefile-цель) и замеры iOS (AC #4). Swap /swapfile-gradle 3 ГБ добавлен в /etc/fstab.
<!-- SECTION:NOTES:END -->
