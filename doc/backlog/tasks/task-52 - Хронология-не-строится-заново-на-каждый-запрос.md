---
id: TASK-52
title: Хронология не строится заново на каждый запрос
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:54:00Z'
updated_date: '2026-11-11T12:09:00Z'
labels:
  - core
  - review
dependencies:
  - TASK-46
priority: medium
type: enhancement
ordinal: 52000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-11 (doc/review-2026-11-11/photos.md P3, P7; Н11 прошлого ревью): каждое открытие сетки и пульта, каждый опрос GET /piles (раз в 3 с, ради имён участников) и закреплений (раз в 1 с) читают всю проекцию: декодирование CBOR всех AssetMeta и сортировка. Замер на x86: обработчик timeline 20 мс при 600 файлах и 285 мс при 10 тыс., JSON 3,6 МБ при 10 тыс.; на телефоне в 2–4 раза дольше. Требование — сетка открывается мгновенно при ~10 тыс. объектов.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Повторный запрос хронологии без изменений pile не читает проекцию заново; любое применённое изменение pile видно в следующем запросе
- [x] #2 GET /piles и опросы закреплений не читают все файлы pile
- [x] #3 Замеры до и после (600 и 10 тыс. файлов) записаны в задаче; поведение покрыто тестами; doc/adr и architecture.md (A58) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Тесты поведения (красные/зелёные до оптимизации): любое применённое изменение pile (своё и от пира: удаление файла, новая ревизия, thumbhash, имя участника) видно в следующем запросе хронологии и GET /piles.
2. Кеш готовой хронологии в node: результат Timeline хранится вместе с каналом Changed реплики, взятым до чтения; повтор без изменений не читает БД. Без инкрементальной проекции.
3. pile.info() (GET /piles) читает только имя pile и участников, без assets (отдельный запрос в store).
4. Опрос закреплений получает хронологию из кеша (core/show без изменений).
5. ETag/304 для GET timeline — только если просто: тело кешируется по тому же каналу Changed, ETag — хеш тела.
6. Замеры до/после на 600 и 10 тыс. (бенчмарк ревьюера), doc/adr и A58.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Замер ДО (master 2059f8c, x86 общей машины, бенчмарк ревьюера core/show/review_bench_test.go + замер GET /piles; не закоммичен, REVIEW=1 go test -run TestReviewTimings -v ./show/; среднее 10 прогонов после прогрева):
| Файлов | Node.Timeline | GET timeline | JSON | GET /piles | show.Pins | show.Next |
| 600 | 12,0 мс | 15,6 мс | 217 КБ | 12,5 мс | 15,3 мс | 25,0 мс |
| 10000 | 189 мс | 257 мс | 3,6 МБ | 163 мс | 163 мс | 204 мс |

Замер ПОСЛЕ (тот же бенчмарк + GET /piles + повтор с If-None-Match; машина под нагрузкой других агентов, load 6–9, шум до 2×):
| Файлов | Node.Timeline | GET timeline 200 | GET timeline 304 | GET /piles | show.Pins | show.Next |
| 600 | 0,2–0,4 мкс | 5,6–6,9 мс | 0,33 мс | 0,4–2,2 мс | 3,5–11,7 мс | 14–52 мс |
| 10000 | 0,2–0,35 мкс | 26,6 мс | 0,21 мс | 0,27–1,5 мс | 30–71 мс | 74–236 мс |
Промежуточно (только кеш Node.Timeline, без кеша JSON): GET timeline 3,9 мс (600) и 62 мс (10 тыс.) — остаток был сборкой и сериализацией JSON; с кешем тела 200-ответ — это передача 3,6 МБ по loopback.
Что осталось O(N) в памяти: show.Pins — loader.setItems (карты и prefetch.SetItems) и plan() на каждый опрос; show.Next — refresh → setItems → plan. Это core/show (TASK-51 и соседние), не трогал. Числа Next сильно шумят (фоновые рендеры T2 при 10 тыс.).
Канал Changed у реплики при синхронизации закрывается после всей пачки GET /ops (defer notify в replica.fetch), поэтому ops от пира видны в хронологии после окончания пачки, а не по одной, как раньше. Это совпадает с семантикой A32 (follow), тестом TestTimelineAndPileListShowEveryAppliedChange покрыто «видно после применения».
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Node.Timeline кеширует готовую отсортированную хронологию на pile вместе с каналом replica.Changed, взятым до чтения: пока канал тот же, повтор не читает БД (GET timeline, опросы закреплений, Start и команды показа получают её из кеша без изменений в core/show). pile.info() для GET /piles читает только имя и участников (store.Info, общий с View запрос участников). Обработчик GET /piles/{id}/timeline кеширует JSON-тело тем же способом и отдаёт ETag (sha256 тела), If-None-Match → 304 (openapi обновлён; приложение пока заголовок не шлёт). Тесты: TestTimelineAndPileListShowEveryAppliedChange (своё и от пира: новый файл, thumbhash, имя участника, новая ревизия, удаление — видно в следующем запросе хронологии и списка pile), TestRepeatedTimelineAndPileListDoNotReadEveryFile (повтор без изменений — меньше аллокаций, чем файлов; до правки 22 тыс. на 1000 файлов), TestUnchangedTimelineIsNotSentAgain (304 и новый ETag после изменения). 10 тыс. файлов, x86: Node.Timeline 189 мс → <1 мкс, GET timeline 257 → 27 мс (304 — 0,2 мс), GET /piles 163 → 0,3–1,5 мс, show.Pins 163 → 30–70 мс (остаток — loader.setItems и plan в core/show). A58, adr/cache.md.
<!-- SECTION:FINAL_SUMMARY:END -->
