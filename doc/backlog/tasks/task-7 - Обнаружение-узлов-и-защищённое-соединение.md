---
id: TASK-7
title: Обнаружение узлов и защищённое соединение
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - core
  - network
  - ios
  - android
milestone: m-0
dependencies:
  - TASK-3
  - TASK-6
documentation:
  - architecture.md
priority: high
type: feature
ordinal: 7000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Участники должны находить друг друга в одной сети без ввода адресов. Go на мобильных не может сам делать mDNS и перечислять интерфейсы (на iOS нужен NWBrowser, на Android 11+ net.Interfaces не работает), поэтому discovery и список интерфейсов отдаёт нативный слой. Соединение — взаимный TLS на самоподписанных ключах устройств плюс доказательство членства с привязкой к каналу (architecture.md, 7.1).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Android и iPhone в одной Wi-Fi-сети находят друг друга через mDNS и NSD (_pileup._tcp) без ввода адресов
- [ ] #2 Если mDNS недоступен, соединение устанавливается по адресу из QR
- [ ] #3 Узел, не знающий секрета pile, не проходит handshake
- [ ] #4 Тест: ретрансляция handshake через посредника не проходит благодаря привязке к TLS-каналу
- [ ] #5 В Info.plist объявлены NSLocalNetworkUsageDescription и NSBonjourServices; на Android включается MulticastLock
<!-- AC:END -->
