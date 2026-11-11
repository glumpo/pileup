---
id: TASK-44
title: 'iOS: обнаружение Bonjour и жизненный цикл приложения'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:20:00Z'
updated_date: '2026-11-11T21:47:00Z'
labels:
  - ios
  - app
  - net
milestone: m-0
dependencies: []
priority: high
type: feature
ordinal: 44000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
iPhone должен находить участников и экраны без ввода адресов, как Android (TASK-7, A22): объявлять `_pileup._tcp` и искать `_pileup._tcp`, `_googlecast._tcp`, `_airplay._tcp`. Отдельно — жизненный цикл: система забирает сокеты приостановленного приложения (Apple TN2277), а цикл Accept порта pile в ядре после ошибки не перезапускается, поэтому после возврата из фона iPhone перестал бы отвечать участникам до перезапуска приложения (architecture-review.md §4.4). Локальная сеть на iOS требует разрешения пользователя; отказ должен быть понятен.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 DiscoveryProvider на iOS объявляет сервис и находит сервисы трёх типов; Found отдаёт имя, адрес, порт и TXT в формате DNS-SD, Lost — имя
- [ ] #2 После возврата приложения из фона объявление mDNS и поиск восстановлены, порты pile проверены и снова принимают соединения (Node.Reconnect, A55), подключения к участникам идут сразу; ошибки Bonjour попадают в журнал диагностики (mobile.Report, A60)
- [ ] #3 Запросы UI к ядру по http://127.0.0.1 и загрузка картинок оттуда разрешены политикой ATS; остальной cleartext не разрешён сверх необходимого
- [ ] #4 Отказ в доступе к локальной сети виден пользователю с подсказкой, как его включить, и не выдаётся за «участник в другой сети»
- [ ] #5 Пока приложение уходит в фон во время раздачи, оно доживает короткое время, а по истечении участники получают обрыв сразу, а не ждут таймаут
- [ ] #6 Чистая логика (TXT, разбор результатов) покрыта unit-тестами на macOS-раннере; doc/adr/ios.md и architecture.md (A50) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Ядро: DiscoveryHandler.Denied(bool) — натив сообщает о запрете локальной сети; Node.LocalNetworkDenied, событие discovery в журнале; join при запрете — local_network_denied; GET /local-network. Красные тесты Go, затем реализация.
2. Приложение: компонент LocalNetworkBanner (новый файл), подключение одной строкой в PileScreen, Join, ScreenPicker; текст local_network_denied в ui.tsx. Красные jest-тесты, затем реализация.
3. Swift Discovery: DNSServiceRegister для объявления (порт чужого сокета), NWBrowser для поиска (состояния ready/waiting(PolicyDenied) → Denied), DNSServiceResolve + DNSServiceGetAddrInfo (IPv4) для адреса, TXT как есть; restart() — пересоздать всё; ошибки в MobileReport.
4. Жизненный цикл в PileupCoreModule: willEnterForeground → Core.restartDiscovery (только mDNS; порты и подключения — JS /health → /reconnect, A54/A55); didEnterBackground → beginBackgroundTask, по истечении Core.stop() синхронно на главном потоке (участники получают обрыв; при возврате JS видит мёртвый /health и перезапускает ядро — запасной путь и для ECONNABORTED).
5. Тесты Swift: unit (разбор результатов, адреса, состояния), интеграция на симуляторе (объявление+поиск в одном процессе, restart, stop).
6. CI: проверка ATS в Info.plist после prebuild.
7. doc/adr/ios.md, A50; merge master; три зелёных задания.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Лид, 2026-11-11: объём уточнён после ревью. Перезапуск слушателя порта pile в ядре уже сделан в TASK-49 (A55): Node.Reconnect проверяет каждый порт pile подключением к себе и переоткрывает его; связь UI с ядром проверяется приложением при возврате на передний план (TASK-48, A54). В этой задаче остаётся нативная сторона. Добавлены находки ревью D5 (запрет «Локальной сети» неотличим от «другой сети») и V5 п. 2 (фон во время раздачи) — doc/review-2026-11-11/diagnosability.md, video.md.
<!-- SECTION:NOTES:END -->
