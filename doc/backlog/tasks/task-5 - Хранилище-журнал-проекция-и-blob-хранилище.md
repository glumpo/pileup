---
id: TASK-5
title: 'Хранилище: журнал, проекция и blob-хранилище'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:37:00Z'
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
