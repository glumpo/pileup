---
id: TASK-23
title: 'Ревью R01+R13: скрытый QR и изоляция ошибок записей в UI'
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-10T04:10:00Z'
updated_date: '2026-11-10T04:16:00Z'
labels:
  - app
  - review
dependencies: []
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: medium
type: bug
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, R01 (P2): приглашение с секретом показывается при открытии экрана pile. R13 (P2): ошибка даты или thumbhash одной записи ломает весь экран.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Приглашение раскрывается только по явному действию и скрывается при уходе с экрана
- [ ] #2 Некорректная дата или thumbhash одной записи показываются локальной заглушкой, остальные записи и навигация работают
- [ ] #3 Каждая ошибка сначала воспроизведена красным тестом
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. R01: пользовательский тест — открытие pile не запрашивает и не показывает приглашение; кнопка раскрывает QR, текст и «Отправить приглашение»; после ухода с экрана и возврата приглашение снова скрыто. Переписать ожидание в piles.test.tsx. Красный → исправить PileScreen (запрос invite только по кнопке, состояние живёт в экране) → зелёный.
2. R13: пользовательские тесты хронологии и полноэкранного просмотра с исправными записями, записью с некорректной taken_at, записью с битым thumbhash и записью, ломающей рендер; некорректная дата первым элементом; восстановление после замены данных. Красный.
3. R13: time.ts возвращает «Дата неизвестна» вместо исключения; Preview перехватывает ошибку thumbhash (console.warn, без заглушки-картинки, T1 грузится); маленькая Error Boundary на запись в сетке, на страницу и подпись в просмотре со сбросом при смене записи. Зелёный.
4. tsc, npm test, make check; заметки в задаче.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
R01: красный тест — piles.test.tsx «a created pile opens with its invite hidden until it is asked for» (Expected false, Received true: QR и строка приглашения видны сразу после открытия, запрос /invite уходит при монтировании); «a shown invite is hidden again after leaving the pile» и «an invite that cannot be made is explained» — нет кнопки «Показать приглашение». Старое ожидание «a created pile opens with its invite as a QR code» закрепляло автоматический показ вопреки 7.2 и заменено. Исправление: PileScreen запрашивает приглашение только по кнопке «Показать приглашение»; состояние живёт в экране, при уходе (экран размонтируется) приглашение снова скрыто.
<!-- SECTION:NOTES:END -->
