---
id: TASK-5
title: 'Хранилище: журнал, проекция и blob-хранилище'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:49:00Z'
labels:
  - core
milestone: m-0
dependencies:
  - TASK-4
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Узлу нужно надёжно хранить журналы операций и состояние между перезапусками, а также превью. Инварианты протокола (непрерывный префикс seq, L для lamport) держатся только если операция и счётчики пишутся атомарно. SQLite берём на чистом Go (modernc.org/sqlite), чтобы не усложнять кросс-компиляцию под gomobile.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Операция, новый seq и новый L записываются в одной транзакции; тест с прерыванием записи не оставляет рассогласованного состояния
- [ ] #2 Проекция в SQLite совпадает с эталонным состоянием crdt на случайно сгенерированных журналах
- [ ] #3 Blob хранится по хешу содержимого, повторная запись того же содержимого не создаёт копию
- [ ] #4 Данные и состояние переживают перезапуск приложения
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. core/store: SQLite (modernc.org/sqlite), один файл БД на pile. Таблицы: ops (журнал, raw байт-в-байт, PK author+seq), pile (L и LWW-регистр имени), members, claims (заявки на файлы для правила владельца A7). Apply/Create в одной транзакции: Decode → Check с head из БД → вставка операции → L = max(L, lamport) → обновление проекции. API повторяет crdt/state: Apply, Create, Ops, View (возвращает state.View).
2. Тесты store: property-тест (rapid) — store и эталонный state получают одинаковые действия (Create, доставка чужих операций, дубликаты и пропуски, переоткрытие БД) и дают одинаковые view, ошибки и байты операций; тест перезапуска; тест прерывания: подпроцесс пишет операции и убивается SIGKILL в случайный момент, после переоткрытия журнал непрерывен, view совпадает с эталоном, пересобранным из сохранённых операций, новые операции принимаются.
3. core/blob: content-addressed хранилище на ФС по SHA-256: Put(io.Reader) пишет во временный файл и атомарно переименовывает, Open, Has. Тесты: хеш, повтор без копии, частичная запись не видна, ошибка чтения не оставляет blob, переживает переоткрытие.
4. gofmt, go vet, go test -race, make check (лицензии новых зависимостей).
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Сделано:
- core/store: один файл SQLite на pile (WAL, synchronous=FULL, одно соединение). Таблицы ops (журнал, raw байт-в-байт), pile (L и LWW-регистр имени), members, claims (заявки на файлы, владелец по A7 вычисляется запросом). Apply/Create: Decode → Check с head из БД → вставка операции → L = max(L, lamport) → проекция, всё в одной транзакции. API повторяет crdt/state, View возвращает state.View.
- Правила редьюсера продублированы в SQL; расхождение с crdt/state ловит property-тест (проверено мутациями: каждая из 5 порч правил роняет тест). Тест прерывания с SIGKILL ловит запись операции и счётчиков в разных транзакциях.
- AssetMeta хранится в claims как CBOR (cbor.Marshal по умолчанию), чтобы nil и пустые []byte и указатели сохранялись как при декодировании.
- Путь к БД передаётся драйверу как есть: с префиксом file: SQLite декодировал %XX в имени файла.
- core/blob: файлы <sha256-hex> в каталоге, запись через tmp/ и rename; tmp/ очищается при Open. Новые зависимости: modernc.org/sqlite и её транзитивные (BSD-3-Clause, MIT), make check проходит.
<!-- SECTION:NOTES:END -->
