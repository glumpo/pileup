---
id: TASK-19
title: Минимальный диагностический лог
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T21:40:00Z'
labels:
  - core
  - app
  - diag
milestone: m-0
dependencies:
  - TASK-3
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Сетевые проблемы между разными телефонами и ТВ сложно отлаживать без журнала событий на устройстве. Лог локальный, не реплицируется; полноценная диагностика — на этапе 4.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Ядро пишет события discovery, подключений, синхронизации, таймаутов и ошибок экранов в кольцевой буфер
- [ ] #2 Лог можно посмотреть в приложении
- [ ] #3 Лог можно экспортировать файлом для баг-репорта
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. core/diag: ограниченный кольцевой буфер событий (время, вид, pile, пир, сообщение, число повторов) на узел; повтор одинакового события сливается с прежним и поднимается наверх; ошибки-таймауты получают вид timeout; текстовый экспорт → тесты буфера
2. replica.Pull сообщает число операций каждого успешного раунда (для событий синхронизации) → тест
3. core/node: события подключений (анонс, входящие рукопожатия, вход по приглашению), синхронизации (установлена, получено N операций, ошибки, форк), обмена адресами, загрузки blob, рендеров и thumbhash; Node.Diag() → тесты на реальных узлах
4. httpapi: GET /diag (JSON, новые сверху), GET /diag/export (text/plain, attachment); api/openapi.yaml → тесты
5. Приложение: экран «Журнал» с экспортом через Share, вход с экрана списка pile → jest
6. gofmt, go vet, go test -race, make check
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
core/diag: буфер на узел (Node.Diag()), 1000 событий; повтор одинакового события (вид, pile, пир, текст) увеличивает счётчик и поднимается наверх — так ретраи раз в секунду не вытесняют остальное. Ошибки с таймаутом получают вид timeout. replica.Pull получил колбэк числа операций раунда. События узла: listening, announce/announce received, incoming handshake, join, sync established/received N ops/ошибки sync (в т. ч. форк), new peer address, render/fetch/original/thumbhash.
<!-- SECTION:NOTES:END -->
