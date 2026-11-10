---
id: TASK-27
title: Загрузка файла идёт напрямую к владельцу, без перебора пиров
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-10T12:43:00Z'
updated_date: '2026-11-10T13:06:00Z'
labels:
  - core
dependencies: []
documentation:
  - adr/cache.md
priority: high
type: bug
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
dialOwner в core/node/blobs.go не знает адрес владельца и перебирает все адреса пиров pile по очереди (5 с на адрес, 20 с на весь запрос, включая тело ответа): мёртвые адреса съедают лимит, к посторонним устройствам делаются лишние handshake, при нескольких выключенных телефонах владелец недостижим. Ключ собеседника подтверждается при каждом соединении синхронизации и announce, поэтому сопоставление «участник → адрес» доступно, но не сохраняется.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Узел помнит адрес каждого участника по подтверждённым соединениям (Pull, announce в обе стороны, вход по приглашению) и загружает файл только с адреса владельца
- [ ] #2 Неизвестный или недоступный адрес владельца сразу даёт ErrUnavailable без подключений к другим устройствам
- [ ] #3 После смены адреса владельца загрузка идёт по новому адресу
- [ ] #4 Лимит 20 с — на подключение и заголовки; тело ответа ограничено таймаутом простоя чтения, а не общим сроком
- [ ] #5 doc/adr/cache.md обновлён
- [ ] #6 Каждая ошибка сначала воспроизведена красным тестом
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Красные тесты (core/node/owner_test.go): мёртвые и зависшие адреса перед владельцем не задерживают загрузку и не получают соединений; неизвестный адрес владельца сразу даёт ErrUnavailable без соединений; после announce с нового адреса загрузка идёт по нему; медленное, но непрерывное тело длиннее лимита заголовков дочитывается, а остановившееся дольше таймаута простоя падает; файл пригласившего грузится с его адреса сразу после входа.
2. Узел хранит в памяти pile отображение участник → адрес; источники: исходящий announce (перед каждым Pull), входящий announce (IP соединения + объявленный порт, ключ из peer.Conn), пригласивший при входе. Побеждает последнее подтверждённое; при смене адреса простаивающие соединения к владельцам закрываются. В операции и piles.json адреса не попадают, после перезапуска отображение заполняет первый announce.
3. dialOwner соединяется только с известным адресом владельца, проверяет ключ; иначе ErrUnavailable.
4. Таймауты: 20 с на соединение и заголовки, тело — таймаут простоя чтения 10 с (обрыв → ErrUnavailable).
5. Обновить doc/adr/cache.md; gofmt, vet, race x10, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красные тесты (core/node/owner_test.go, коммит 9caa5d6) на старом коде:
- TestFetchGoesOnlyToOwnerAddress: T2 владельца за 3 с не загружен (context deadline exceeded) — перебор упирается в молчащие адреса по 5 с.
- TestFetchFromOwnerWithUnknownAddressFailsWithoutConnecting: context deadline exceeded вместо быстрого ErrUnavailable.
- TestFetchFollowsOwnerToNewAddress: после announce с нового адреса T2 ушёл на старый (1 запрос на старый, 0 на новый).
- TestBodyIsLimitedByReadIdleTimeoutNotByTotalTime: steady — тело обрезано общим таймаутом (Client.Timeout while reading body); stalled — ошибка не ErrUnavailable.
- TestInviterFilesAreFetchedFromInviterRightAfterJoin на старом коде зелёный (перебор находил пригласившего), красный на промежуточной реализации без источника «вход» (owner address is unknown).

Реализация: pile.addrs (память, под n.mu) — участник → последний подтверждённый адрес; источники: исходящий announce (attempt), входящий announce (IP соединения + порт, ключ из peer.Conn через ConnContext), reach при входе. При смене адреса закрываются простаивающие соединения к владельцам pile. dialOwner — только этот адрес, 5 с, проверка ключа (errNotOwner). download: 20 с на соединение и заголовки (таймер отменяет контекст), тело — 10 с без байтов (readIdle); ошибки чтения тела теперь ErrUnavailable. Хранить отображение на диске не стали: первый announce после старта его восстанавливает.
Лимиты в тестах подменяются node.SetFetchTimeouts.

Доработка: входящий POST /announce больше не источник адреса владельца. Он только запускает исходящий цикл к IP соединения + объявленному порту; адрес запоминается после успешного исходящего announce, то есть когда он достижим с нашей стороны. Красный тест TestIncomingAnnounceAloneDoesNotSetOwnerAddress на прежнем коде: announce с недостижимого адреса подменял рабочий (T2: connection refused); участник, известный только по входящему announce, загружался до нашего announce (err = nil вместо ErrUnavailable). Риск об односторонней достижимости из doc/adr/cache.md снят. Тест смены адреса ждёт sync established с новым адресом.
<!-- SECTION:NOTES:END -->
