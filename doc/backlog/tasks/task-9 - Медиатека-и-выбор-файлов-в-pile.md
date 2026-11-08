---
id: TASK-9
title: Медиатека и выбор файлов в pile
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:39:00Z'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: интерфейс MediaProvider для нативного слоя, выбор по датам, исключения, публикация AssetAdd/AssetRemove, API для UI. Нативные PhotoKit/MediaStore — позже.
<!-- SECTION:NOTES:END -->
