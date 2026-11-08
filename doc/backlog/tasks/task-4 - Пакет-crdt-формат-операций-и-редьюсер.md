---
id: TASK-4
title: 'Пакет crdt: формат операций и редьюсер'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:33:00Z'
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
- [x] #1 Конверт операции содержит pile_id, author, seq, prev, lamport, wall_time, schema, type, payload и подпись ed25519; подпись проверяется над байтами операции как есть
- [x] #2 Операция с seq, не равным N+1 для известного префикса автора, отвергается
- [x] #3 lamport новой операции равен L+1, при получении чужой L = max(L, lamport); в журнале одного автора lamport строго растёт, иначе операция отвергается
- [x] #4 Операция неизвестного типа сохраняется и пересылается, но не влияет на состояние
- [x] #5 AssetAdd и AssetRemove принимаются только от владельца файла; Join проверяет proof от секрета pile
- [x] #6 Property-тесты: одинаковое состояние при любом чередовании журналов разных авторов и при повторной доставке
- [x] #7 Fuzz-тест декодера не находит паник; golden vectors фиксируют формат
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
- crdt/op: конверт CBOR [body, sig], Body с целыми ключами 1..9, Decode проверяет подпись над байтами body как есть и разбирает payload известных типов; Validator.Check: pile_id, seq, prev, lamport, Join на seq 1.
- crdt/clock, crdt/vv (Missing).
- crdt/state: Apply/Create/Ops/View; владелец файла — автор AssetAdd с наименьшей парой (lamport, author), чтобы владелец не зависел от порядка доставки.

- Golden vectors: core/crdt/op/testdata/*.hex (join, pile_create, member_set, asset_add, asset_remove, unknown_type), пересборка из фиксированных ключей совпадает байт в байт; перегенерация: go test ./crdt/op -run Golden -update.
- Fuzz (по 60 с): FuzzDecode 2.25M exec, FuzzSignedBody 1.28M exec, FuzzPayload 0.80M exec — паник нет.
- Property-тесты rapid: 100 прогонов по умолчанию, локально прогнано по 10000 (TestAnyDeliveryOrderGivesSameState, TestNodesConvergeAfterSync) — расхождений нет.

Проверка владельцем: gofmt, go vet, go test -race и make check проходят. Решения A4–A8 записаны в architecture.md.
Открытые риски для следующих задач: (1) участник может опубликовать lamport = MaxUint64, после чего L+1 переполняется — защиты нет; (2) две разные операции с одним (author, seq) отвергаются как ErrSeq и не сообщаются как форк — нужно в TASK-8; (3) Decode и View разделяют срезы с вызывающим кодом; (4) в View пустые []byte и nil различаются при DeepEqual — учитывать в TASK-5.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Реализован чистый пакет core/crdt: op (конверт [body, sig] в детерминированном CBOR, подпись ed25519 над body, id = SHA-256(body), изменения PileCreate/Join/MemberSet/AssetAdd/AssetRemove, Validator.Check со структурными правилами seq/prev/lamport/Join), clock (Lamport), vv (version vectors и Missing), state (эталонная in-memory модель: Apply/Create/Ops/View).
Проверка: юнит-тесты на каждое правило, property-тесты rapid (сходимость при любом чередовании и дублях, 10 000 прогонов локально), golden-векторы для всех типов, fuzz трёх целей по 60 с без находок; go test -race и make check проходят.
Решения на ревью: A4–A8. Риски: переполнение lamport, отсутствие явного сигнала о форке, разделяемые срезы.
<!-- SECTION:FINAL_SUMMARY:END -->
