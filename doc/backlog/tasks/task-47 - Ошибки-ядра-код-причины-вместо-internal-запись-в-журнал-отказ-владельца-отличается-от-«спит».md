---
id: TASK-47
title: >-
  Ошибки ядра: код причины вместо internal, запись в журнал, отказ владельца
  отличается от «спит»
status: Done
assignee:
  - '@claude'
created_date: '2026-11-11T01:41:00Z'
updated_date: '2026-11-11T11:41:00Z'
labels:
  - core
  - app
  - review
dependencies:
  - TASK-46
priority: high
type: bug
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-11 (doc/review-2026-11-11/diagnosability.md D1, D2, D8, D9; reliability.md R2-05): у ядра нет единого формата ошибки. Всё неклассифицированное уходит в UI как 500 internal («Ошибка ядра: internal»), текст причины теряется и в журнал не попадает. Ошибка рендера на телефоне владельца (файл удалён из галереи, не декодируется) выдаётся за «нет ответа — разбудите телефон» и перезапрашивается раз в 2 с бесконечно. Нехватка места на диске нигде не называется, а загрузчик показа считает собственную ошибку записи недоступностью источника, и пульт советует будить чужой телефон. Сбой связи UI с ядром показывается как TypeError: Network request failed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Каждый ответ 500 ядра записывается в журнал диагностики с маршрутом и текстом ошибки; на экране вместо «internal» — понятный текст с отсылкой к журналу
- [x] #2 Ошибка на стороне владельца (ответ владельца с ошибкой) приходит в UI отдельным кодом, не как source_unavailable; просмотр пишет, что телефон участника не смог подготовить файл, и не перезапрашивает раз в 2 с
- [x] #3 Нехватка места (ENOSPC, SQLITE_FULL) даёт код disk_full в API; просмотр, «Мои файлы», закрепления и пульт называют причину и что сделать; загрузчик показа не помечает источник недоступным из-за собственной ошибки записи
- [x] #4 Ошибки медиатеки (нет доступа, файл исчез из галереи, не удалось отрендерить свой файл) имеют свои коды и тексты
- [x] #5 Сбой связи приложения с ядром и 401 показываются понятным текстом, а не TypeError или internal
- [x] #6 Каждое изменение поведения сначала воспроизведено красным тестом (Go или jest); doc/adr и architecture.md (A53) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Go, красные тесты: httpapi — 500 пишется в журнал (маршрут, текст), ошибка медиатеки -> media_failed, свой рендер -> render_failed, отказ владельца -> source_failed; на заполненном tmpfs (linux, root, иначе skip) — disk_full в API и в загрузчике показа (прогресс закреплений и подсказка disk_full вместо timeout у источника).
2. Jest, красные тесты: тексты internal/disk_full/media_failed, source_failed без повтора, render_failed, unknown_asset в «Моих файлах», disk_full в закреплениях и пульте, TypeError связи с ядром и 401, вид журнала api.
3. Ядро: node.DiskFull (ENOSPC, SQLITE_FULL), ErrMedia/ErrRender оборачивание, первая строка ответа владельца в ошибке; httpapi.fail(w, r, err) пишет 500/507 в журнал (вид api); blob: ErrRefused раньше ErrUnavailable; loader: своя ошибка записи — Broken + отметка места.
4. UI: таблица кодов в ui.tsx, Viewer, SelectionScreen, Pins, Remote (подсказка места), Log (вид api).
5. doc/adr, architecture.md A53; полный прогон go test/vet, tsc, jest.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Решения по кодам (заводил только для причины со своим текстом и действием):
- disk_full (507): node.DiskFull — ENOSPC или SQLITE_FULL; на tmpfs проверены оба: GET blob → «write …: no space left on device», POST /piles → «database or disk is full (13)».
- media_failed (500) вместо предложенного ревью media_denied: ядро видит только «MediaProvider.Media вернул ошибку», отказ в разрешении от прочего не отличить; текст советует разрешение как вероятную причину.
- render_failed (500): ошибка своего Rendition. Отдельного asset_gone для рендера нет — натив не различает «файл удалён» и «не декодируется»; текст называет обе причины.
- Файл исчез из галереи при переключении в «Моих файлах» — существующий unknown_asset (Decide), новый код не нужен: свой текст и перечитывание списка в приложении.
- source_failed (502): ErrRefused проверяется раньше ErrUnavailable; ErrRefused по-прежнему оборачивает ErrUnavailable (тест TestOwnAssetWithoutRenditionMakerIsUnavailable не меняется), просмотр не повторяет. В ошибку попадает первая строка ответа владельца.
- Журнал 500/507: вид api, «<r.Pattern>: <err>», pile из {id}; лог передаётся через контекст запроса (обёртка ResponseWriter потеряла бы ReaderFrom у ServeContent).
- Нехватка места в показе: подсказка о себе (author = свой ключ, reason disk_full) и Reason прогресса закреплений, 5 минут (prefetch.Recheck) или до первой удачной загрузки.
- D9: только describe (TypeError «Network request failed» и 401); api.ts не трогал (TASK-48).
Тесты на заполненный диск монтируют tmpfs: только linux и root, иначе t.Skip — в CI (ubuntu, не root) пропускаются; локально проходят.
Один раз видел флак jest «found screens are listed…» (show.test) при полном прогоне под нагрузкой; отдельно и в двух повторных полных прогонах зелёный.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Ядро отвечает кодом причины вместо internal и пишет каждую 500/507 в журнал (вид api: маршрут, pile, текст ошибки). Новые коды: disk_full (ENOSPC/SQLITE_FULL), media_failed, render_failed, source_failed (ответ владельца с ошибкой больше не source_unavailable). Загрузчик показа не винит источник за свою ошибку записи: файл «битый» на 5 мин, подсказка и прогресс закреплений — disk_full. Приложение: тексты internal (с отсылкой к «Журналу»), disk_full, media_failed, 401, сбоя связи с ядром; просмотр — source_failed/render_failed без повтора; «Мои файлы» — файл исчез из галереи, список перечитывается; закрепления и пульт называют нехватку места; вид журнала «Ошибка». Тесты: Go httpapi (errors_test.go, diskfull_linux_test.go), show (diskfull_linux_test.go), jest (errors.test.tsx, viewer, pins, show). Риск: тесты заполненного диска требуют root (tmpfs) и в CI пропускаются. doc/adr/cache.md, api/openapi.yaml, architecture.md A53.
<!-- SECTION:FINAL_SUMMARY:END -->
