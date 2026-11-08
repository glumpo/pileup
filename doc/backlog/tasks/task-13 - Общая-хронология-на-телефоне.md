---
id: TASK-13
title: Общая хронология на телефоне
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T20:59:00Z'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Ядро: Node.Timeline(id) — все файлы pile, отсортированные по (taken_at, author, asset_id), с именами авторов; GET /piles/{id}/timeline в httpapi и api/openapi.yaml → проверка: Go-тест на двух узлах с чередующимися фото и видео обоих участников, одинаковый порядок у обоих.
2. Приложение: типизированный клиент локального ядра (src/api.ts), связка с нативным модулем в одном месте (src/core.ts: start/port/token, requestMediaPermissions).
3. Экраны без библиотек навигации (стек маршрутов в App.tsx + BackHandler): список pile и создание; pile с QR-приглашением; вход по QR (expo-camera) и вставкой текста; выбор (диапазон дат, мои файлы с переключателем); сетка хронологии (FlatList строк по дням, getItemLayout); полноэкранный просмотр T2 с листанием (горизонтальный FlatList pagingEnabled).
4. Превью: thumbhash (пакет thumbhash, MIT) → data URL, поверх T1 через blob-эндпоинт с заголовком Authorization; плейсхолдер убирается после загрузки.
5. Тесты: jest-expo + @testing-library/react-native с поддельным ядром (fetch и нативный модуль): создание pile, вход по тексту приглашения, порядок и авторы в сетке, плейсхолдер до загрузки, листание, синтетическая сетка из 5000 элементов; npx tsc --noEmit.
6. Проверки: gofmt, go vet, go test -race, npm test, make check (лицензии новых зависимостей).
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начаты: API хронологии в ядре и экраны приложения (список pile, создание, приглашение QR, вход, выбор, сетка, полноэкранный просмотр). Проверка на устройстве — позже.
<!-- SECTION:NOTES:END -->
