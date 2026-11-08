---
id: TASK-13
title: Общая хронология на телефоне
status: To Do
assignee: []
created_date: '2026-11-08T06:49:00Z'
labels:
  - app
milestone: m-0
dependencies:
  - TASK-10
  - TASK-12
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Главный экран приложения: файлы всех участников в одной хронологии, чтобы одни и те же события не приходилось смотреть несколько раз с разных телефонов. Дни — только визуальные разделители, в модели их нет.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Сетка показывает файлы всех участников, отсортированные по (taken_at, author, asset_id); порядок одинаков на обоих телефонах
- [ ] #2 У каждого файла виден автор
- [ ] #3 До загрузки миниатюры показывается thumbhash
- [ ] #4 Сетка из 5000 элементов скроллится плавно (допустимы синтетические данные)
- [ ] #5 Файл открывается на весь экран и листается вперёд и назад
<!-- AC:END -->
