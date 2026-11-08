---
id: TASK-9
title: Медиатека и выбор файлов в pile
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:52:00Z'
labels:
  - app
  - media
  - ios
  - android
milestone: m-0
dependencies:
  - TASK-3
  - TASK-5
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 9000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Участник вносит в pile только выбранное: другим должно быть видно лишь то, что он явно отдал. Доступ к медиатеке есть только у нативного слоя (PhotoKit, MediaStore), ядро получает данные через MediaProvider.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Участник выбирает диапазон дат; фото и видео из диапазона публикуются как AssetAdd
- [ ] #2 По умолчанию исключаются скриншоты и скрытые альбомы
- [ ] #3 Отдельный файл можно исключить вручную; исключение публикуется как AssetRemove и файл пропадает у всех
- [ ] #4 GPS и прочие данные вне AssetMeta не покидают устройство
- [ ] #5 Работает на iPhone и на Android, включая фото из iCloud без локального оригинала
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Go-часть (нативные PhotoKit/MediaStore — позже):
1. core/media: интерфейс Provider для нативного слоя — Media(from, to int64) ([]byte, error), JSON-массив Item (local_id, kind, сырые времена, размеры, длительность, размер, mime, codec, имя, modified, screenshot, hidden). Только типы, допустимые gomobile.
2. core/node/media.go: Select(ctx, pile, from, to) — перечислить через провайдер, посчитать taken_at (media.TakenAt), оставить файлы с taken_at в [from, to), по умолчанию без скриншотов и скрытых альбомов, учесть ручные решения; asset_id = sha256(author, local_id), revision = sha256(modified). Сравнить с моими опубликованными файлами и опубликовать одну AssetAdd с добавленными/изменёнными и одну AssetRemove с выбывшими. Диапазон и ручные решения хранятся локально (piles/<id>.selection.json) и сохраняются до публикации.
3. Node: Selection(pile) — кандидаты диапазона с флагом «опубликован»; Decide(ctx, pile, asset, selected) — ручное исключение/возврат; Assets(pile) — файлы pile у всех.
4. HTTP: PUT /piles/{id}/selection {from,to}, GET /piles/{id}/selection, PUT /piles/{id}/selection/{asset_id} {selected}; обновить api/openapi.yaml.
5. Тесты с фейковым провайдером: публикация диапазона и приём другим узлом, исключения по умолчанию, ручное исключение у всех, смена диапазона публикует только разницу, GPS/local_id не попадают в байты операций, файл без локального оригинала выбирается как обычный.
6. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: интерфейс MediaProvider для нативного слоя, выбор по датам, исключения, публикация AssetAdd/AssetRemove, API для UI. Нативные PhotoKit/MediaStore — позже.

Go-часть сделана (ветка task-9):
- node.MediaProvider: Media(from, to int64) ([]byte, error) — JSON-массив node.MediaItem (local_id, kind photo|video|live_photo, platform_time, exif_date_time, exif_offset, video_creation_time, file_time, width, height, duration_ms, size_bytes, mime, codec, original_name, modified, screenshot, hidden). Провайдер может вернуть больше диапазона; ядро само фильтрует по taken_at в [from, to). Неизвестные поля (GPS, путь, альбом) отбрасываются при разборе. gobind проверен: Java byte[] media(long, long) throws Exception, ObjC media:to:error: → NSData. В mobile интерфейс ещё не проброшен: фасад пока без node (A13).
- Node: Select(ctx, pile, from, to), Decide(ctx, pile, asset, selected), Selection(pile), Assets(pile); Config.Media.
- asset_id = SHA-256(pubkey устройства ‖ local_id), revision = SHA-256(modified, int64 BE), thumbhash пустой до TASK-11.
- Публикуется разница с моими файлами в проекции: одна AssetRemove и одна AssetAdd (новые и изменившиеся, например после правки — новый revision). Диапазон и ручные решения — piles/<id>.selection.json, пишутся до публикации. Ручное решение перекрывает умолчание (скриншот можно вернуть).
- HTTP: GET/PUT /piles/{id}/selection, PUT /piles/{id}/selection/{asset}; ошибки unknown_asset (404), no_media (501).
- Файл без локального оригинала (iCloud) выбирается как обычный: метаданные от провайдера, отдельного поля нет; превью — TASK-11/12.
<!-- SECTION:NOTES:END -->
