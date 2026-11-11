---
id: TASK-42
title: 'iOS: Go-ядро в приложении и сборка на macOS-раннере'
status: To Do
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
labels:
  - ios
  - app
  - core
  - ci
milestone: m-0
dependencies: []
priority: high
type: feature
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
iOS-часть TASK-3 не сделана: Swift-модуль `app/modules/core/ios` — заглушка шаблона, ядро не подключено, сборки под iOS нет. Mac нет, есть только macOS-раннеры GitHub; логи заданий публичного репозитория без авторизации недоступны (API отвечает 403), поэтому раннер должен сам отдавать логи туда, где их можно прочитать по ssh. Без рабочего цикла «push → сборка → логи» остальные iOS-задачи (медиатека, обнаружение, AirPlay) вести нельзя. Образец — Android-часть TASK-3 и решения A13, A21, A31.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 CI на macOS-раннере собирает ядро в xcframework (`make mobile-ios`) и приложение под симулятор iOS без подписи; логи шагов читаются без авторизации в GitHub
- [ ] #2 Swift-модуль даёт тот же JS API, что Android: start, stop, port, token; seed ключа устройства хранится в Keychain без переноса в бэкап и на другое устройство, каталог данных ядра исключён из бэкапа
- [ ] #3 NetworkInfo отдаёт адреса интерфейсов устройства; сотовые интерфейсы исключены
- [ ] #4 Чистая логика Swift покрыта unit-тестами, которые запускаются на macOS-раннере
- [ ] #5 В CI приложение запускается на симуляторе, ядро стартует и отвечает на /health с токеном; без токена — отказ
- [ ] #6 Время сборки и размер приложения с ядром записаны в задаче; решения записаны в architecture.md (A-таблица)
<!-- AC:END -->
