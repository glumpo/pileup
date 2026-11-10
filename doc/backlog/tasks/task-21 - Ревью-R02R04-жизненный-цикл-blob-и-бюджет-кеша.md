---
id: TASK-21
title: 'Ревью R02+R04: жизненный цикл blob и бюджет кеша'
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T04:10:00Z'
updated_date: '2026-11-10T05:12:00Z'
labels:
  - core
  - review
dependencies: []
documentation:
  - review-2026-11-09-fixed.md
  - architecture.md
priority: high
type: bug
ordinal: 21000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-09, пункты R02 (с R03) и R04. Вытеснение удаляет физический файл по хешу без учёта других ссылок (другой pile, закрепление, T1 с теми же байтами); удаление может проиграть гонку завершению загрузки; T2, загруженные через viewer, не попадают в бюджет; старые ревизии и удалённые файлы остаются на диске.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Физический blob удаляется только после снятия последней удерживающей ссылки; вытеснение в одном pile не ломает файл, нужный другому pile, закреплению или T1 с тем же хешем
- [x] #2 Удаление во время загрузки не приводит к повторному появлению объекта; конкурентные запросы одного объекта выполняют одну загрузку
- [x] #3 Любой полученный T1/T2 (viewer, показ, прогрев, ручная загрузка) учитывается в бюджете и вытесняется по общей политике без участия вызывающего кода
- [x] #4 Удалённые файлы и старые ревизии очищаются
- [x] #5 Каждая ошибка сначала воспроизведена красным тестом
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Проверить утверждения R02/R04 по коду; зафиксировать подтверждённые и отклонённые.
2. Красные тесты на текущем коде: (a) вытеснение в pile B удаляет T2, закреплённый в pile A; (b) T1 и T2 с одинаковыми байтами; (c) два asset с одинаковым содержимым; (d) T2 через HTTP-путь viewer не учитывается в бюджете; (e) удаление asset во время загрузки — объект появляется снова; (f) удалённый asset и старая ревизия остаются на диске. Отдельный коммит.
3. blob.Store как единый кеш узла: cache.db (objects: ключ (owner, asset, revision, tier) → hash, size; holds: удержание pile с taken_at; pins), счётчик физического содержимого по hash в транзакции под мьютексом, single-flight попытки со своим context/cancel/done, отбрасывание позднего результата, ожидание очистки, бюджет и вытеснение T2 по pile (ранний taken_at первым, окно и pins защищены), Retain для удалённых/старых ревизий, Close с ожиданием попыток. Тесты сценариев R02.4 (1, 4, 7, 9, 12×3, 13, 14, 15, 18) и R04 на fake-загрузках с каналами.
4. node: Blob/Cached через кеш, Held/Protect/Budget, pins в cache.db с переносом pins.json, retain при изменении pile; Evict и refs убрать.
5. show/prefetch: планировщик получает состояние кеша из узла (SetCached), окно передаётся как защита; учёт размера и вытеснение из prefetch убрать.
6. Обновить существующие тесты, gofmt/vet/race (новые тесты -count=10), make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Проверка утверждений ревью по коду:
- Подтверждено: SetRef после загрузки без повторной проверки asset; загрузчик показа использует общий контекст host; Evict требует текущий asset/revision и не может убрать удалённые файлы/старые ревизии; Evict удаляет файл по hash без учёта других ссылок; ref не содержит pile, а asset_id = digest(owner, local_id), поэтому один файл владельца в двух pile имеет один ключ и общий ref; loader.setItems проверяет наличие T2 один раз на ключ; T2 из viewer (httpapi → Node.Blob) не попадает в учёт.
- Дополнительно найдено: собственные T2 владельца, отрендеренные для участников (serveBlob), тоже хранились без бюджета; одновременные Blob одного ключа у участника делали две сетевые загрузки.
Красные тесты (коммит 7c28016) до исправления:
- (a) TestEvictionInOnePileKeepsDisplayPinnedInAnother: «eviction in one pile dropped the file pinned in another».
- (b) TestEvictedDisplayKeepsThumbnailWithSameBytes: T1 после вытеснения T2 — «node: asset owner is unavailable».
- (c) TestEvictedFileKeepsOtherFileWithSameBytes: «file in the window is not cached» (общий файл удалён).
- (d) TestDisplaysOpenedInViewerAreKeptWithinBudget: «only the latest files within the budget stay: timed out».
- (e) TestAssetRemovedWhileLoadingDoesNotReappear: «err = <nil>, want ErrUnknownAsset» (объект опубликован после удаления).
- (f) TestRemovedAssetAndOldRevisionLeaveNothingOnDisk: «...are gone on the owner: timed out».
После 1bc2216 все шесть зелёные.
Решения: cache.db в каталоге blobs; старые refs/ и файлы без записи в базе удаляются при открытии (кеш заполнится заново), pins.json переносится в базу и удаляется; бюджет 1 ГиБ на pile сохранён (A26), задаётся в node.Config.Budget; учёт по различным hash внутри pile; собственные T2 владельца тоже в бюджете pile; T1 вне бюджета, удерживается, пока asset текущий в pile; держатель-читатель — открытый дескриптор (POSIX unlink не мешает дочитать).

Проверка владельцем: влито в master, тесты и make check проходят. Решение A30 записано. Риски: нет состояния cleanup_failed (сценарий 16) — ошибки вытеснения повторяются при следующем событии; файл, удалённый с диска извне, недоступен до перезапуска; просмотренный во viewer ранний файл может сразу вытесниться при превышении бюджета (курсора viewer нет).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Единый кеш узла (blob.Store + blobs/cache.db): логические объекты, подсчёт ссылок на физическое содержимое, удержания pile и закрепления в одной транзакционной БД; одна попытка загрузки на ключ с собственным контекстом и корректной отменой; бюджет T2 на pile и вытеснение внутри кеша для любого пути запроса (viewer, показ, прогрев, ручная загрузка); очистка удалённых файлов и старых ревизий.
Проверка: шесть дефектов воспроизведены красными тестами в отдельном коммите (7c28016) и исправлены; юнит-тесты сценариев жизненного цикла на управляемых каналах и testing/synctest; интеграционные тесты общего учёта и рестарта. Решение A30.
<!-- SECTION:FINAL_SUMMARY:END -->
