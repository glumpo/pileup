---
id: TASK-5
title: 'Хранилище: журнал, проекция и blob-хранилище'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:34:00Z'
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
