---
id: TASK-26
title: 'Ревью R10+R12: диапазон Seq/Lamport, отказ от schema, версия приложения в Join'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-10T04:10:00Z'
updated_date: '2026-11-10T04:39:00Z'
labels:
  - core
  - protocol
  - review
dependencies:
  - TASK-22
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: medium
type: task
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, R10 (P2): uint64 Seq/Lamport против знакового INTEGER SQLite, переполнение L+1. R12 (P2, согласовано): убрать schema из операций, развивать протокол новыми типами, добавить AppVersion в Join. Формат меняем до релиза, пока это дёшево.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Seq и Lamport вне согласованного диапазона отклоняются до сохранения; следующая запись не переполняется
- [ ] #2 Операции создаются без schema; неизвестный тип после Join сохраняется и пересылается, известные операции после него применяются
- [ ] #3 AppVersion из Join доступна в данных участника; Join без версии допустим
- [ ] #4 Изменения сначала зафиксированы красными тестами
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Проверить утверждения R10/R12 по коду (op.go, change.go, validate.go, store.go, state, clock, replica).
2. R10: диапазон Seq/Lamport [1, 2^63−1] (знаковый INTEGER SQLite). Тесты: валидатор на границах (2^63−1 принимается, 2^63 и 2^64−1 — ErrRange), store и state: входящая операция с Lamport 2^63−1 принимается, следующая локальная запись явно отклоняется (ErrRange) без сохранения и без изменения L; 2^63 отклоняется до сохранения → красный → проверка диапазона в Validator.Check → зелёный.
3. R12: тесты на уровне формата: New не пишет ключ 7 (schema); известные типы без schema декодируются и применяются; поле schema (1 и 2) игнорируется; Join → неизвестный тип → известный: байты хранятся и пересылаются без изменений (replica), последняя известная операция действует; старые golden-векторы со schema=1 декодируются, проходят проверку и применяются в store → красный → удалить Schema из Body и проверку в decodeChange → зелёный. Перегенерировать golden-векторы, прежние сохранить как testdata/schema1.
4. AppVersion: Join.AppVersion (CBOR-ключ 4, необязательный), state.Member/members.app_version (миграция существующих БД добавлением столбца), node.Config.AppVersion → Join, mobile.Start(dir, platform, appVersion, …), Kotlin передаёт versionName; API members[].app_version (openapi). Тесты: подпись покрывает версию, Join без версии допустим, версия доходит до View/HTTP API/mobile, старая БД открывается.
5. gofmt, go vet, go test -race, make check, Kotlin unit-тесты, make android-debug; заметки, коммиты.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
R10 подтверждён с уточнением: входящая операция с Lamport 2^63−1 сохраняется и поднимает L; следующая локальная запись подписывает Lamport 2^63 и падает на INSERT с ошибкой драйвера (database/sql: uint64 values with high bit set are not supported) — явная ошибка, не тихая порча; входящие 2^63…2^64−1 тоже отвергаются только драйвером при INSERT, а не валидацией. Эталонная модель crdt/state принимала любые uint64 (расхождение со store), а clock.Next при 2^64−1 молча переполняется в 0. Диапазон: Seq и Lamport в [1, 2^63−1]; проверка в Validator.Check до сохранения (ErrRange, структурная ошибка по A6). При L = 2^63−1 вычисление L+1 в uint64 не переполняется, локальная запись явно отклоняется ErrRange, ничего не сохраняется.
Красные тесты R10: TestSeqAndLamportStayWithinSignedInt64 (Check вернул nil для Lamport 2^63, 2^64−1, Join с 2^63, Seq 2^63), store TestLamportBoundIsCheckedBeforeStorage (ошибка драйвера вместо ErrRange), state TestLamportBoundIsCheckedBeforeApplying (Lamport 2^63 принят).
<!-- SECTION:NOTES:END -->
