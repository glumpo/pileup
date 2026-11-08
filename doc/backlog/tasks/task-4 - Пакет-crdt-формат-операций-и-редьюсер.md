---
id: TASK-4
title: 'Пакет crdt: формат операций и редьюсер'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:13:00Z'
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
