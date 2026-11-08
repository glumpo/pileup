---
id: TASK-7
title: Обнаружение узлов и защищённое соединение
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T22:29:00Z'
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
- [x] #3 Узел, не знающий секрета pile, не проходит handshake
- [x] #4 Тест: ретрансляция handshake через посредника не проходит благодаря привязке к TLS-каналу
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

Обнаружение (AC #1, #2, #5):
1. core/node: интерфейс DiscoveryProvider (Advertise(service, name, port, txt), Browse(service, handler), Stop) и DiscoveryHandler (Found(name, host, port, txt), Lost(name)), реализуемый в Go; TXT — проводной формат DNS-SD (RFC 6763). Поле Config.Discovery, nil — обнаружения нет → verify: существующие тесты node проходят без изменений.
2. Каждый pile объявляется как _pileup._tcp со случайным именем и TXT pile=<hex первых 16 байт SHA-256(pile_id)>; узел ищет _pileup._tcp, свои объявления (по имени) игнорирует, найденные адреса с хешем своего pile добавляет в пиры; Join пробует адреса из приглашения, затем найденные по хешу → verify: тесты с фейковой шиной в памяти (вход по устаревшему адресу из QR, чужой pile, своё объявление, TXT без id и секрета).
3. Экраны: поиск _googlecast._tcp и _airplay._tcp, GET /screens (id, name, kind, address, available, reason); AirPlay доступен только на платформе ios, иначе причина по architecture.md 2.2; openapi.yaml → verify: тесты httpapi для android и ios.
4. core/mobile.Start принимает DiscoveryProvider → verify: тест mobile, экраны из нативного провайдера.
5. Android: NSD-провайдер в Kotlin с MulticastLock (CHANGE_WIFI_MULTICAST_STATE через config plugin); чистая логика (TXT, очередь resolve, удержание lock) — JVM-тесты → verify: :core:testDebugUnitTest, make android-debug.
6. iOS: NSLocalNetworkUsageDescription и NSBonjourServices в app.json (ios.infoPlist) → verify: expo prebuild в временную копию, ключи в Info.plist.
7. gofmt, go vet, go test -race, make check без ANDROID_HOME.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: TLS на ключах устройств и handshake членства с привязкой к каналу. Нативное обнаружение (NWBrowser/NSD), Info.plist и MulticastLock — после TASK-3.

Go-часть соединения: пакет core/peer (Dial, NewListener, Conn с Peer/PileID/Handshake); K_auth берётся из op.AuthKey.
Решения, требующие ревью: экспортёр TLS с меткой RFC 9266 «EXPORTER-Channel-Binding», пустой контекст, 32 байта; MAC = HMAC-SHA256(K_auth, exporter ‖ pubkey из TLS-сертификата отправителя). Формат: клиент → [len(pile_id) 1 байт][pile_id][MAC клиента 32]; сервер проверяет и отвечает [MAC сервера 32]; при ошибке соединение закрывается без сообщения. pile_id — до 255 байт, в открытом виде внутри TLS. Сертификат: срок 1970–9999, даты не проверяются (CA нет, часы телефонов не доверенные). Таймауты handshake: у клиента — из ctx, у сервера handshake ленивый (при первом Read/Write, как в crypto/tls), таймаут задаёт вызывающий (например, http.Server).
Тесты: успех и HTTP поверх соединения, чужой секрет у каждой стороны, неизвестный pile, MITM-ретрансляция (свой ключ и украденный ключ клиента без S — второй случай ловит только привязка к каналу), отражение proof клиента, поддельная подпись сертификата, отказ от TLS 1.2; мутационная проверка — каждая проверка в коде ловится тестом.

Go-часть влита в master (adf193d), решение A11 записано. Остаются AC #1, #2, #5: нативное обнаружение (NWBrowser/NSD), адрес из QR, Info.plist и MulticastLock.

Начата оставшаяся часть: интерфейс DiscoveryProvider в ядре, объявление и поиск _pileup._tcp, список экранов (_googlecast._tcp, _airplay._tcp), Android NSD с MulticastLock, ключи Info.plist через конфиг.

Обнаружение сделано в ядре и на Android (коммиты 94fdc08, f004a93, 6f0c663).
Ядро: node.DiscoveryProvider {Advertise(service, name, port, txt), Browse(service, handler), Stop}, обработчик реализует Go: DiscoveryHandler {Found(name, host, port, txt), Lost(name)}; TXT — проводной формат DNS-SD (RFC 6763) байтами. Config.Discovery = nil — обнаружения нет, всё работает по адресам из QR (существующие тесты без изменений). mobile.Start получил последний параметр discovery.
Поведение: каждый pile объявляется как _pileup._tcp на своём порту, TXT ровно pile=<hex первых 16 байт SHA-256(pile_id)>; имя экземпляра pileup-<hex первых 8 байт SHA-256(pubkey ‖ pile_id)> — стабильно и по нему узел узнаёт свои объявления, в том числе оставшиеся в кеше сети после перезапуска. Найденный узел с хешем моего pile добавляется в пиры (как после /announce, аутентификация — core/peer); вход по QR сначала пробует найденные по хешу адреса, затем адреса из приглашения. При Close провайдер останавливается.
Экраны: поиск _googlecast._tcp и _airplay._tcp, GET /screens (id, name, kind cast|airplay, address, available, reason). Имя Cast — из TXT fn, иначе имя экземпляра; AirPlay — имя экземпляра. AirPlay доступен только при platform=ios, иначе reason «Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника.». Lost убирает экран. openapi.yaml обновлён.
Тесты Go с фейковой шиной в памяти: вход при устаревшем адресе в QR, переподключение после смены портов у обоих, чужой pile и своё объявление не становятся пирами, своё объявление из кеша после перезапуска игнорируется, TXT только с хешем и снят после Close; httpapi — Cast доступен, Apple TV на android недоступен с пояснением, на ios доступен, ушедший экран пропадает; mobile — провайдер подключён. Мутационная проверка: каждое условие ловится тестом.
Android: NsdDiscovery (NsdManager): resolve строго по одному (до API 34 параллельный resolve запрещён), имя берётся из onServiceFound, а не из resolve; MulticastLock без счётчика ссылок захватывается при первом объявлении или поиске и отпускается в stop; разрешение CHANGE_WIFI_MULTICAST_STATE в config plugin. Чистая логика (TXT, Session, SerialQueue) — JVM-тесты DiscoveryTest.
iOS: NSLocalNetworkUsageDescription и NSBonjourServices (_pileup._tcp, _googlecast._tcp, _airplay._tcp) задаёт config plugin; проверено expo prebuild --platform ios во временную копию. Swift-провайдера нет: iOS-модуль пока заглушка (TASK-3), поэтому AC #1 для iPhone и проверка на устройствах остаются.
Решения на ревью: порт только в SRV-записи, в TXT не дублируется (отступление от формулировки 7.1); хеш — 16 байт SHA-256 без доменного разделителя; найденные адреса копятся в piles.json так же, как от /announce, без удаления устаревших; ошибки провайдера игнорируются (обнаружение — необязательный путь).
Риски: _airplay._tcp объявляют и HomePod/Mac/ТВ с AirPlay 2 — фильтр по TXT features/model не сделан; resolveService и NsdServiceInfo.host устарели в API 34 (работают); в будущих Android может понадобиться разрешение на локальную сеть; на реальных устройствах не проверено.

Проверено: gofmt -l пусто, go vet ./..., go test -race ./... (все пакеты), make check без ANDROID_HOME (Go, лицензии, tsc, jest 14/14), :core:testDebugUnitTest (DiscoveryTest 10/10 и прежние), make android-debug — BUILD SUCCESSFUL, в собранном манифесте есть CHANGE_WIFI_MULTICAST_STATE.
<!-- SECTION:NOTES:END -->
