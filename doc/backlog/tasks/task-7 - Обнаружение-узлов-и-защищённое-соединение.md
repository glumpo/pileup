---
id: TASK-7
title: Обнаружение узлов и защищённое соединение
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:56:00Z'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
План покрывает только Go-часть соединения (критерии #3 и #4); discovery, QR-адреса, Info.plist и MulticastLock — отдельно, после TASK-3.
1. Экспортировать из crdt/op функцию AuthKey(secret) — единственное место вывода K_auth; формат операций и golden vectors не меняются → verify: go test ./crdt/...
2. Пакет core/peer (имя net конфликтует со stdlib): самоподписанный X.509-сертификат на ключе устройства ed25519, взаимный TLS 1.3 без CA, проверка, что сертификат собеседника подписан его же ключом.
3. Handshake членства внутри TLS: клиент называет pile, обе стороны шлют HMAC(K_auth, tls_exporter ‖ own_pubkey) (метка экспортёра RFC 9266), при ошибке соединение закрывается. Результат — net.Conn / net.Listener для будущего HTTP.
4. Тесты по спецификации: успех (pubkey и pile id у обеих сторон), чужой секрет у клиента и у сервера, неизвестный pile, MITM-ретрансляция (свой ключ и украденный ключ без S), сертификат с несовпадающей подписью, отказ от TLS 1.2.
5. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: TLS на ключах устройств и handshake членства с привязкой к каналу. Нативное обнаружение (NWBrowser/NSD), Info.plist и MulticastLock — после TASK-3.
<!-- SECTION:NOTES:END -->
