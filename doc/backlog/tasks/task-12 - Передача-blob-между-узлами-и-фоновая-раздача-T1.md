---
id: TASK-12
title: Передача blob между узлами и фоновая раздача T1
status: Done
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T13:10:00Z'
labels:
  - core
  - network
milestone: m-0
dependencies:
  - TASK-8
  - TASK-11
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Источник только анонсирует свои файлы и отдаёт их по запросу; что и когда загружать, решает потребитель (architecture.md, 6.2). Миниатюры нужны у всех участников, чтобы общая сетка работала без задержек.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Blob доступен по /blob/{asset_id}/{revision}/{tier} с Cache-Control immutable и ETag по хешу содержимого
- [x] #2 Запросы с Range поддерживаются, в том числе для видео-оригиналов
- [x] #3 Каждый участник фоном загружает T1 всех файлов pile
- [x] #4 Источник никогда не отправляет данные по своей инициативе
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Пакет blob: ссылки имя → хеш (refs) — состояние кеша, отображение (владелец, asset, revision, tier) → blob, вне CRDT. Тест: ссылка переживает переоткрытие, перезапись.
2. Пакет store: Asset(id) — один файл из проекции без чтения всего View. Тест: совпадает с View, включая удалённые и чужие.
3. core/node/blobs.go: интерфейс RenditionMaker (gomobile-типы: Rendition(localID, tier) []byte, OriginalSize, ReadOriginal(offset, length)), Config.Renditions и Config.LocalID; отдача GET /blob/{asset}/{revision}/{tier} участникам pile по peer: только свои файлы, T1/T2 лениво генерируются и кладутся в blob-хранилище, T3 стримится с Range без сохранения; Cache-Control immutable, ETag. Неизвестное → 404.
4. Потребитель: фоновый цикл на pile — T1 всех чужих файлов pile у владельца (до 5 параллельно, повтор при ошибке, по сигналу изменений реплики), в локальное blob-хранилище. Источник ничего не шлёт сам.
5. Node.Blob и GET /piles/{id}/blob/{asset}/{revision}/{tier} (T1/T2) для UI: из локального хранилища или загрузка у владельца; openapi.yaml.
6. Тесты по критериям: заголовки и ETag = хеш, Range (частичные и полное чтение оригинала), многоузловой тест фоновой загрузки T1, тест «источник не шлёт без запроса». gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: реестр превью, отдача blob по /blob/{asset}/{rev}/{tier} с Range поверх peer, фоновая загрузка T1 потребителем. Нативная генерация превью (TASK-11) — через интерфейс.

Go-часть сделана (ветка task-12).
Интерфейс node.RenditionMaker (gomobile-типы): Rendition(localID, tier) ([]byte, error) для T1/T2; OriginalSize(localID) и ReadOriginal(localID, offset, length) для T3. Config.Renditions и Config.LocalID(assetID) → (localID, ok): отображение asset_id → platform local id ведёт медиатека (TASK-9), здесь только шов. Константы node.T1/T2/T3.
Кеш: blob-хранилище одно на узел (<Dir>/blobs), отображение (владелец, asset_id, revision, tier) → хеш — файлы-ссылки blob.Store.SetRef/Ref (<Dir>/blobs/refs/sha256(имени)), вне CRDT и SQLite.
Peer: GET /blob/{asset_hex}/{revision_hex}/{1|2|3} на listener pile. Отдаются только свои текущие файлы (владелец по проекции = я, revision совпадает, LocalID знает файл), иначе 404. T1/T2 создаются при первом запросе и сохраняются; T3 стримится через ReadOriginal кусками по 1 МиБ, не сохраняется. Cache-Control: max-age=31536000, immutable; ETag T1/T2 = SHA-256 содержимого, T3 = revision (хешировать оригинал на каждый запрос нельзя). Range, multipart, If-None-Match, If-Range — http.ServeContent.
Потребитель: цикл на pile — T1 всех чужих файлов из проекции, до 5 загрузок параллельно, по сигналу replica.Changed и повтор через 1 с при ошибке; сетевая ошибка владельца пропускает его остальные файлы до следующего прохода. Владельца ищем перебором известных адресов пиров с проверкой его ключа в peer-рукопожатии.
UI: GET /piles/{id}/blob/{asset}/{revision}/{1|2} — из локального хранилища, иначе генерация (свой файл) или загрузка у владельца с сохранением; ошибки unknown_asset (404), source_unavailable (502). store.Asset(id) — один файл из проекции.
Риски: без discovery (TASK-7) участники, вошедшие по одному приглашению, не знают адресов друг друга и не получат T1 друг друга (только через пригласившего — он отдаёт лишь свои); T2, загруженные для UI, не вытесняются, пока не встроен prefetch; файл с постоянной ошибкой у владельца повторяется раз в секунду.

Ветка task-12 слита в master (merge --no-ff), конфликты с TASK-9 в node.Config, httpapi.WithNode, openapi.yaml разрешены с сохранением обеих функций; ErrUnknownAsset один (media.go).
LocalID: Config.LocalID убран. Config = Dir, Keystore, Platform, Addrs, Media, Renditions. Соответствие asset_id (hex) → local id для опубликованных своих файлов пишется при Select/Decide в piles/<id>.selection.json (поле local, только на устройстве) и держится в памяти pile. Неизвестный asset — ErrUnknownAsset (404), Renditions == nil — ErrUnavailable (без паники).
Тесты: тестовый Publish удалён, файлы публикуются через Select с фейковой медиатекой; httpapi-тест больше не пишет операции в БД. Остался тестовый Stored (наблюдать фоновую загрузку без запроса). Случаи «файла нет на телефоне» убраны: опубликованный файл теперь всегда имеет local id. Убраны случайно закоммиченные файлы падений rapid.
Обмен адресами (AC #3): ответ на POST /announce — JSON-список известных адресов пиров, кроме адреса объявившегося; он добавляет их к своим пирам. Адреса только в piles.json, не в операциях. Тест на трёх узлах: B и C входят через A, каждый получает T1 другого без запросов UI.
Риски: dialOwner перебирает пиров последовательно (до 5 с на недоступного); устаревшие адреса не удаляются; один узел под двумя адресами (несколько интерфейсов) может попасть в пиры дважды или сам к себе.

Проверка владельцем: go vet, тесты и make check проходят; решение A18 записано. .gitignore исправлен на **/testdata/rapid/ (раньше шаблон совпадал только с корнем).
Риски: удалённый на платформе файл даёт 500 вместо 404 и опрашивается каждую секунду; поиск владельца перебирает пиров по очереди с таймаутом 5 с; устаревшие адреса не удаляются; узел с несколькими интерфейсами может попасть в список дважды или тянуть сам у себя.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Передача blob между участниками: интерфейс RenditionMaker для нативного слоя, peer-маршрут /blob/{asset}/{rev}/{tier} (только свои файлы, T1/T2 лениво с сохранением, T3 стримом), Range/ETag/immutable через http.ServeContent, фоновая загрузка T1 всеми участниками (до 5 параллельно, повторы), UI-маршрут /piles/{id}/blob/... через локальное ядро. После слияния с TASK-9 local id своих файлов берётся из selection.json; добавлен обмен адресами участников через ответ на /announce.
Проверка: тесты на immutable/ETag/304, Range (включая видео-оригинал и multipart), фоновую загрузку T1 у двух и у трёх участников, отсутствие отправки данных без запроса. Решение на ревью: A18.
<!-- SECTION:FINAL_SUMMARY:END -->
