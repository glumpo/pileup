---
id: TASK-26
title: 'Ревью R10+R12: диапазон Seq/Lamport, отказ от schema, версия приложения в Join'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T04:10:00Z'
updated_date: '2026-11-10T05:12:00Z'
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
- [x] #1 Seq и Lamport вне согласованного диапазона отклоняются до сохранения; следующая запись не переполняется
- [x] #2 Операции создаются без schema; неизвестный тип после Join сохраняется и пересылается, известные операции после него применяются
- [x] #3 AppVersion из Join доступна в данных участника; Join без версии допустим
- [x] #4 Изменения сначала зафиксированы красными тестами
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

R12 подтверждён: New пишет schema=1 (ключ 7), decodeChange распознаёт только schema=1; известная операция без schema становится неизвестной, а первая такая операция (Join) отвергается ErrJoin и блокирует журнал автора.
Красные тесты R12 (на коде до изменения, после добавления полей-заглушек без поведения): TestNewWritesNoSchemaField (ключи тела [1 2 3 5 6 7 8 9]); TestSchemaFieldDoesNotSelectDecoding (все известные типы без schema и со schema=2 → Change=nil, битый payload без schema не отвергался); TestJoinUnknownKnownWithoutSchemaValidate (Join без schema не распознан); replica TestUnknownTypeIsRelayedVerbatimAndLaterOpsApply (ErrJoin на Join без schema); state TestMembers, store TestMemberKeepsAppVersionFromJoin и TestDatabaseOfPreviousVersionOpens, httpapi TestCreateListAndJoinByInvite, mobile TestMemberCarriesAppVersionFromNative (версия приложения не доходит до участника/API). TestLegacyVectorsWithSchemaStillApply и store TestLegacyOpsWithSchemaApply зелёные и до изменения (страховка перехода).
Сделано: поле Schema и константа удалены из Body, выбор обработчика только по type; ключ 7 не занят, остальные номера не менялись; прежнее поле schema игнорируется как неизвестное (байты и подпись сохраняются). Join.AppVersion — CBOR-ключ 4, omitempty, внутри подписанного payload. Golden-векторы перегенерированы (тело без ключа 7, Join с app_version), прежние векторы со schema=1 сохранены в testdata/schema1 и проверяются: декодируются, проходят Validator.Check и применяются в store. state.Member и members.app_version (миграция: ALTER TABLE ADD COLUMN app_version DEFAULT '' для БД прежней версии), node.Config.AppVersion → Join, mobile.Start(dir, platform, appVersion, …), Kotlin передаёт versionName пакета; GET /piles: members[].app_version (пустая строка — версия неизвестна), openapi обновлён. iOS не подключён (модуль-заглушка), версию не передаёт.

Решения для журнала (architecture.md не менялся): 5.1 — поле schema убрано из Op, seq/lamport в диапазоне [1, 2^63−1]; A4 — номера полей тела: 1 pile_id, 2 author, 3 seq, 4 prev, 5 lamport, 6 wall_time, 8 type, 9 payload; ключ 7 (бывший schema) выведен и не переиспользуется; A6 — ErrRange (seq/lamport вне диапазона) — структурная ошибка; A8 — первая часть («schema ≠ 1 → неизвестный тип») заменена: обработчик выбирается только по type, прежнее поле schema игнорируется, несовместимые изменения — новым типом, номера типов и полей не переиспользуются; «неизвестные поля игнорируются» и «перечисления не проверяются» остаются; 5.2 — Join{name, platform, proof, app_version?} (ключ 4, необязательный; нет — версия неизвестна); A9 — БД прежней версии получает столбец members.app_version при открытии; A13 — Start(dir, platform, appVersion, …); A21 — Android передаёт versionName пакета. API: members[].app_version, пустая строка — неизвестна.
Проверки: gofmt, go vet, go test -race ./..., fuzz по 10 с, make check, Kotlin :core:testDebugUnitTest (38 тестов), make android-debug — пройдены.

Проверка владельцем: тесты и make check проходят; решение A31 записано.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
R10: диапазон Seq/Lamport [1, 2^63−1] проверяется до сохранения во всех путях (Validator.Check), локальная запись не переполняется. R12: операции без schema, выбор обработчика по type, старое поле игнорируется, golden-векторы перегенерированы, старые векторы schema=1 сохранены и применяются; Join.app_version доходит до участника и API; Android передаёт versionName.
Проверка: красные тесты до изменения, зелёные после; Kotlin-тесты и android-debug проходят. Решение A31. Риск: клиенты до этой версии несовместимы с новыми Join.
<!-- SECTION:FINAL_SUMMARY:END -->
