---
id: TASK-10
title: Время съёмки taken_at
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:52:00Z'
labels:
  - core
  - media
milestone: m-0
dependencies:
  - TASK-9
documentation:
  - architecture.md
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Хронология строится только по времени съёмки. Фото хранят локальное время без пояса, видео — UTC, поэтому сравнение сырых значений систематически сдвигает видео на величину пояса. Алгоритм выбора taken_at описан в architecture.md, 5.4; его нужно реализовать детерминированно и подтвердить на реальных устройствах — это один из рисков этапа.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Функция в Go выбирает taken_at по правилам 1–6 из раздела 5.4 и записывает taken_at_source; каждое правило покрыто табличным тестом
- [x] #2 Пояс текущего устройства в вычислении taken_at не используется
- [ ] #3 На реальных iPhone и Android фото и видео одного телефона стоят в правильном порядке
- [ ] #4 Фото и видео двух телефонов, снятые вперемешку, стоят в правильном порядке; результаты проверки записаны в задаче
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
План покрывает только AC #1–#2 (Go-часть); AC #3–#4 ждут устройств.
1. Пакет core/media: функция TakenAt(device []op.RawTime) []Time — по сырым временам всех файлов одного устройства возвращает taken_at, taken_at_source и local_offset для каждого по правилам 1–6 раздела 5.4.
2. Разбор сырых значений: EXIF-время в формате EXIF «YYYY:MM:DD HH:MM:SS», смещение «±HH:MM»; битые значения делают правило неприменимым, без паники.
3. Табличный тест на каждое правило, включая провал к следующему правилу на битых значениях; тест независимости заимствования от порядка входа; fuzz на отсутствие паники.
4. Тест AC #2: меняет time.Local процесса на разные пояса и получает те же результаты.
5. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата только Go-часть (функция выбора taken_at и табличные тесты, AC #1–#2). Проверка на реальных устройствах (AC #3–#4) ждёт TASK-9 и устройств.

Go-часть: core/media, func TakenAt(device []op.RawTime) []Time (Time{TakenAt, TakenAtSource, LocalOffsetSec}). Тесты: TestTakenAt (таблица по правилам 1–6), TestDeviceTimeZoneIsNotUsed (та же таблица при разных time.Local), TestBorrowedOffsetDoesNotDependOnInputOrder, FuzzTakenAt.
Решения, принятые без владельца (на ревью):
- EXIF-время принимается только в формате EXIF «YYYY:MM:DD HH:MM:SS» (без пояса, разбирается как UTC); «0000:00:00 00:00:00», пробелы, лишний текст, несуществующая дата — правило неприменимо.
- OffsetTimeOriginal принимается только как «±HH:MM», часы ≤ 14, минуты ≤ 59.
- platform_time и creation_time видео ≤ 0 считаются отсутствующими (getLong у NULL в MediaStore даёт 0; creation_time = 0 в MP4 — это 1904 год).
- Донор для правила 4 — файл того же устройства с разбираемыми EXIF-временем и смещением (каким бы правилом ни получен его taken_at). «Ближайший» — по модулю разницы локальных EXIF-времён; при равенстве побеждает более ранний донор, затем меньшее смещение. Результат не зависит от порядка входа.
- local_offset — известное EXIF-смещение файла при любом правиле, либо заимствованное в правиле 4; иначе нет.
- Если нет даже времени файла, taken_at = 0 с источником file_time: отдельного значения «нет времени» в op.TimeSource нет.

Go-часть влита в master (merge e334e80). Решение A10 записано в architecture.md. Риск: в RawTime нет SubSecTimeOriginal, серийные снимки в пределах одной секунды получают одинаковый taken_at и упорядочиваются по author/asset_id. Остаются AC #3–#4 (реальные устройства, после TASK-9).
<!-- SECTION:NOTES:END -->
