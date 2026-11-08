---
id: TASK-13
title: Общая хронология на телефоне
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T21:30:00Z'
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
- [x] #1 Сетка показывает файлы всех участников, отсортированные по (taken_at, author, asset_id); порядок одинаков на обоих телефонах
- [x] #2 У каждого файла виден автор
- [x] #3 До загрузки миниатюры показывается thumbhash
- [ ] #4 Сетка из 5000 элементов скроллится плавно (допустимы синтетические данные)
- [x] #5 Файл открывается на весь экран и листается вперёд и назад
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

Ядро: Node.Timeline(id) сортирует файлы pile по (taken_at, author, asset_id); GET /piles/{id}/timeline отдаёт массив TimelineItem (id, revision, author hex, author_name, kind, taken_at, local_offset?, duration_ms?, width, height, thumbhash base64, пустой если нет) — описан в api/openapi.yaml. Тест httpapi на двух узлах: чередующиеся фото/видео/live photo обоих участников, совпадение taken_at у разных авторов и у одного автора; хронологии обоих узлов совпадают и идут в порядке спецификации.
Приложение: src/api.ts (клиент ядра), src/core.ts (единственное место вызова нативного модуля, включая requestMediaPermissions), экраны в src/screens, навигация — стек маршрутов в App.tsx + BackHandler, просмотр рисуется поверх сетки (позиция прокрутки сохраняется). Сетка — FlatList строк (заголовок дня или до 4 файлов одного дня) с getItemLayout; день — по taken_at + local_offset, без смещения — по поясу телефона. Превью: thumbhash → PNG data URL под Image с T1 (заголовок Authorization), плейсхолдер убирается после onLoad. Просмотр: горизонтальный FlatList pagingEnabled с T2; видео показываются постером T2, воспроизведения нет.
Зависимости: expo-camera (MIT; тянет barcode-detector и zxing-wasm, MIT), thumbhash (MIT), qrcode-generator (MIT); dev: jest, jest-expo, @testing-library/react-native, @types/jest. QR рисуется View-строками без react-native-svg. Даты выбора — текстовые поля ГГГГ-ММ-ДД в поясе телефона.
Тесты jest (14) с поддельным ядром: создание pile и QR, вход по тексту и по QR, порядок и авторы в сетке, thumbhash до T1, листание, 5000 элементов рендерятся окном (<400 ячеек) и прокручиваются до последнего. Плавность на устройстве не проверена.

Влито в master (526f2ee), решение A20 записано. AC #1 — Go-тест на двух узлах; #2, #3, #5 — тесты экранов против замоканного ядра. Осталось: плавность сетки на 5000 элементах на устройстве (AC #4). Риски: экран pile на телефоне вошедшего показывает имя и участников только после pull-to-refresh; приглашение и участники грузятся одним запросом.
<!-- SECTION:NOTES:END -->
