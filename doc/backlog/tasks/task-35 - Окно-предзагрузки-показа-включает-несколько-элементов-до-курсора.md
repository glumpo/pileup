---
id: TASK-35
title: Окно предзагрузки показа включает несколько элементов до курсора
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T22:58:00Z'
updated_date: '2026-11-10T23:25:00Z'
labels:
  - core
dependencies: []
documentation:
  - architecture-review.md
  - adr/cache.md
priority: high
type: enhancement
ordinal: 35000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Окно — курсор и 20 следующих. Позади курсора ничего не грузится и не защищается. Показ, запущенный из середины («На экран» из полноэкранного просмотра), или прыжок дают холодный кеш позади: каждый «Предыдущий» — полная загрузка у владельца. Требование: «назад на несколько картинок» тоже мгновенно. Н4 в doc/architecture-review.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 В окно входят 5 элементов перед курсором; они планируются после курсора и 20 следующих и защищены от вытеснения так же
- [x] #2 Лимит загрузок на источник и остальные правила планировщика сохраняются
- [x] #3 Каждое поведение сначала воспроизведено красным тестом
- [x] #4 doc/adr/cache.md обновлён
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. core/prefetch: константа Behind = 5; window() после курсора и 20 следующих добавляет до 5 показываемых элементов перед курсором, от ближнего к дальнему. Plan берёт окно по порядку, поэтому элементы позади планируются после 20 следующих и делят лимит на источник; Window() отдаёт их загрузчику для Protect, Waiting считает их как остальное окно.
2. Красные тесты: prefetch_test (порядок в плане, лимит на источник, края хронологии, Skip позади, закреплённые после окна), модель окна в property_test; show: старт из середины кеширует P05..P30 и держит их сверх бюджета. Существующие тесты, опиравшиеся на окно только вперёд, переписать по новой спецификации (прыжок, вытеснение по бюджету, закрепления сверх бюджета, общий учёт viewer/показ/закрепления; evictFirst снимает защиту остановкой показа).
3. doc/adr/cache.md: окно, защита, константы, тесты.
4. go test -race ./... в core.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красные тесты (8035094, 319fcf1), на коде до правки:
- TestLoadsCursorAndNextTwentyThenFiveBeforeInOrder: load = [a010 … a030], want [a010 … a030 a009 a008 a007 a006 a005]
- TestItemsBeforeCursorWaitForNextItemsOfTheirSource (один источник, курсор a050): load = [a070], want [a070 a049 a048 a047 a046]
- TestPinnedItemsShareFiveLoadsPerSource: load before the cursor = [a000 … a004], want [a089 … a085]
- TestWindowIsCursorTwentyNextAndFiveBeforeShownItems: в окне нет a009 a008 a006 a005 a004 (a007 — Skip)
- TestWindowStopsAtEndsOfChronology, TestSkippedCursorItemIsStillLoaded, TestJumpForwardKeepsEarlierCache, TestJumpBackwardLoadsNewWindow, TestPinnedItemsLoadAfterWindowFromCursor: нет тех же элементов перед курсором
- TestInvariantsOverRandomEvents: window [a001], want [a001 a000]
- core/show: TestFiveFilesBeforeCursorAreCachedAndKeptOverBudget — «cursor, 20 next and 5 previous files cached: timed out»; TestNextWindowIsCachedBeforeRendererAsks — «window after a jump: timed out»; TestPinnedFilesAreNotEvictedUntilUnpinned — «unpinned files evicted, pinned kept over budget: timed out»; TestViewerShowAndPinsShareOneAccounting — «window cached: timed out»
- после первой правки: TestWaitingItemsAreInTimelineOrder — waiting = [a010 … a030 a009 … a005], want [a005 … a030]; rapid: waiting [a012 a011], want [a011 a012]

Реализовано: prefetch.Behind = 5. window() после курсора и 20 следующих добавляет до 5 показываемых (не Skip) элементов перед курсором, от ближнего к дальнему, не уходя за начало хронологии. Plan идёт по окну, поэтому элементы позади планируются после 20 следующих и делят с ними лимит PerSource. Window() идёт в loader.plan → Node.Protect, поэтому элементы позади защищены от вытеснения так же, в том числе сверх бюджета. Waiting сортируется по хронологии: контракт waiting и hints в openapi — «in timeline order».

Переписаны тесты, опиравшиеся на окно только вперёд: TestNextWindowIsCachedBeforeRendererAsks — прыжок на P28 (P22 вне окна не грузится); TestCacheEvictsEarliestFilesOverBudget — переход на P10 (окно P05..P29 равно бюджету); TestPinnedFilesAreNotEvictedUntilUnpinned — после Goto C25 держатся C20..C29; TestViewerShowAndPinsShareOneAccounting — пять файлов F0..F4 между C0 и A2 заполняют окно позади, бюджет 9 файлов, A0 по-прежнему держится только учётом бюджета. evictFirst снимает защиту остановкой показа, а не переходом на следующий файл (предыдущий теперь в окне). В TestEvictedFileKeepsOtherFileWithSameBytes Y и Z поменялись местами, чтобы вытеснение остановилось на Y.

Проверка: gofmt, go vet ./...; go test -race ./prefetch -count=3 и -rapid.checks=2000; ./show целиком и -count=5 для затронутых тестов; ./httpapi; go test -race ./... в core — всё зелёное, кроме TestBodyIsLimitedByReadIdleTimeoutNotByTotalTime/stalled в core/node (код не трогался, отдельно -count=5 проходит — нестабильность по времени под нагрузкой).

Ограничения: элементы позади грузятся только после всех 20 следующих того же источника; при медленном источнике «назад» после прыжка прогревается позже. Ждущие и подсказки теперь учитывают и файлы перед курсором. Viewer курсора по-прежнему не задаёт.

Решение для architecture.md: Окно предзагрузки показа — курсор, 20 следующих и 5 предыдущих показываемых элементов (prefetch.Behind). Порядок плана: курсор, +1…+20, затем −1…−5; лимит 5 загрузок на источник общий для всего окна и закреплений; всё окно защищено от вытеснения, как и раньше, в том числе сверх бюджета. waiting и hints остаются в порядке хронологии. Меняет §6.2 п. 2 («окно — 20 элементов после курсора») и п. 3. Почему: показ с середины («На экран» из полноэкранного просмотра) или прыжок оставляли позади холодный кеш, и каждый «Предыдущий» был полной загрузкой у владельца, а требование — «назад на несколько картинок тоже» (Н4). Элементы позади идут после следующих, поэтому последовательный показ вперёд не замедляется. TASK-35.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Окно предзагрузки показа: курсор, 20 следующих и 5 предыдущих показываемых элементов (prefetch.Behind). Элементы позади планируются после 20 следующих, делят лимит 5 загрузок на источник и защищены от вытеснения через тот же Window() → Node.Protect. Waiting отсортирован по хронологии, как обещает контракт waiting/hints. Красные тесты в prefetch (план, лимит, края хронологии, Skip, закрепления, модель rapid) и в show (старт с середины кеширует и держит P05..P30 сверх бюджета); тесты, опиравшиеся на окно только вперёд, переписаны по новой спецификации. doc/adr/cache.md и описание POST /show в openapi обновлены. go test -race ./... в core зелёный, кроме не связанной нестабильности TestBodyIsLimitedByReadIdleTimeoutNotByTotalTime/stalled в core/node. Риск: при медленном источнике файлы позади прогреваются только после 20 следующих.
<!-- SECTION:FINAL_SUMMARY:END -->
