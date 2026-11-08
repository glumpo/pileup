---
id: TASK-14
title: Роль хоста, сессия показа и выбор экрана
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T22:40:00Z'
labels:
  - core
  - app
  - show
milestone: m-0
dependencies:
  - TASK-13
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Показ ведёт один узел — хост; экран общается только с ним, а хост сам ходит к источникам. Сессия показа живёт только на хосте и в CRDT не попадает. Пользователь должен понимать, какие экраны доступны и почему некоторые недоступны (architecture.md, 2.2 и 2.3).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Любой из двух телефонов может стать хостом и запустить показ
- [ ] #2 Хост ведёт сессию: текущий файл, вперёд, назад, пауза для видео; управление с экрана хоста
- [ ] #3 Хост проксирует видео с источника с поддержкой Range
- [ ] #4 Список экранов показывает найденные устройства; недоступный способ показа виден с причиной и подсказкой, а не просто серой кнопкой
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Go-часть (сессия показа на хосте):
1. Пакет core/show: Host с одной активной сессией в памяти (D8). Start(pile, screen, [asset]) — экран из node.Screens(); недоступный экран (AirPlay не с iPhone) — ошибка с пояснением экрана; Cast — подключение core/cast; AirPlay на iPhone-хосте — сессия без Go-рендерера (рисует натив, TASK-18). Курсор по хронологии, next/prev/goto, play/pause для видео, stop.
2. Медиа для экрана: отдельный HTTP-listener на LAN только на время сессии, маршрут /show/{token}/{asset}/{revision}/{2|3}; токен случайный на сессию, отзывается при остановке; T2 — из blob-хранилища хоста или от владельца (node.Blob), T3 — только для видео, стрим с источника через хост с Range (новый node.ServeOriginal). Других маршрутов нет.
3. Прогретый кеш: core/prefetch на курсоре сессии; SetItems из хронологии, Loaded для T2 уже в хранилище (node.Cached), SetOnline по живому pull-соединению с владельцем (node.Online), Plan после каждого события и раз в секунду; загрузка T2 через node.Blob, вытеснение node.Evict; ждущие элементы — в состоянии сессии.
4. Cast: LOAD текущего элемента (image/jpeg для фото, mime для видео) при старте и на каждой смене курсора, PLAY/PAUSE командой, STOP при остановке; ошибки LOAD и обрыв — в состоянии и в журнале диагностики (renderer).
5. HTTP API для UI: POST/GET/DELETE /show, POST /show/next|prev|goto|play|pause; api/openapi.yaml.
6. Тесты: два узла и хост на каждом, фейковый Cast receiver, токен экрана, видео с Range, окно кеша, вытеснение, офлайн-источник, ошибки Cast. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: сессия показа на хосте, раздача медиа экрану, встраивание планировщика кеша (TASK-15) и управление Cast (TASK-17). Интерфейс выбора экрана и пульта — следующим шагом.
<!-- SECTION:NOTES:END -->
