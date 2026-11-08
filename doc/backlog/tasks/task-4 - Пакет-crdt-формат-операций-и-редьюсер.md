---
id: TASK-4
title: 'Пакет crdt: формат операций и редьюсер'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:18:00Z'
labels:
  - core
  - protocol
milestone: m-0
dependencies:
  - TASK-2
documentation:
  - architecture.md
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
crdt — сердце протокола синхронизации, и менять формат после появления реальных данных у пользователей дорого. Пакет чистый, без IO, чтобы протокол был виден целиком и полностью покрывался тестами. На этапе 1 нужен минимальный набор изменений: PileCreate, Join, MemberSet, AssetAdd, AssetRemove. Правила seq, lamport и prev описаны в architecture.md, раздел 5.1.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Конверт операции содержит pile_id, author, seq, prev, lamport, wall_time, schema, type, payload и подпись ed25519; подпись проверяется над байтами операции как есть
- [ ] #2 Операция с seq, не равным N+1 для известного префикса автора, отвергается
- [ ] #3 lamport новой операции равен L+1, при получении чужой L = max(L, lamport); в журнале одного автора lamport строго растёт, иначе операция отвергается
- [ ] #4 Операция неизвестного типа сохраняется и пересылается, но не влияет на состояние
- [ ] #5 AssetAdd и AssetRemove принимаются только от владельца файла; Join проверяет proof от секрета pile
- [ ] #6 Property-тесты: одинаковое состояние при любом чередовании журналов разных авторов и при повторной доставке
- [ ] #7 Fuzz-тест декодера не находит паник; golden vectors фиксируют формат
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Зависимости: fxamacker/cbor/v2 (MIT), pgregory.net/rapid (MPL-2.0, только тесты) → verify: make check (лицензии)
2. crdt/op: конверт [body, sig] в детерминированном CBOR с целыми ключами; Body{pile_id, author, seq, prev, lamport, wall_time, schema, type, payload bstr}; id = SHA-256(body); Decode (конверт, тело, подпись над байтами body как есть, разбор payload известных типов); Sign/New для создания; Validator(pile_id, S): pile_id, seq = N+1, prev, строгий рост lamport, seq 1 = Join с proof = HMAC(HKDF(S, "pileup-auth"), pubkey), Join только на seq 1 → verify: unit-тесты на каждую причину отказа
3. Типы изменений PileCreate, Join, MemberSet, AssetAdd{[]AssetMeta}, AssetRemove{[]asset_id}; AssetMeta по 5.3 без GPS
4. crdt/clock: L, Next = L+1, Observe = max; crdt/vv: {author → N}, вычисление недостающего у пира → verify: unit-тесты
5. crdt/state: эталонная in-memory модель: Apply(raw) с валидацией, Create (seq, prev, lamport = L+1), хранение и пересылка байт-в-байт, View для сравнения с другой реализацией (TASK-5); LWW имени по (lamport, author); владелец файла = автор первого AssetAdd по (lamport, author), действует последняя операция владельца; неизвестные типы хранятся без эффекта → verify: unit-тесты по правилам
6. Property-тесты rapid: сходимость при любом чередовании журналов и повторной/преждевременной доставке → verify: go test
7. Golden vectors в testdata, fuzz декодера (≥60 с на цель) → verify: go test -fuzz
8. gofmt, go vet, go test -race, make check
<!-- SECTION:PLAN:END -->
