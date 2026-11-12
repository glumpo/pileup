---
id: TASK-58
title: >-
  Телефон-источник не засыпает во время раздачи видео; видео важнее массовой
  загрузки
status: Done
assignee:
  - '@claude'
created_date: '2026-11-11T01:54:00Z'
updated_date: '2026-11-12T18:04:00Z'
labels:
  - core
  - app
  - review
dependencies:
  - TASK-51
  - TASK-46
priority: medium
type: enhancement
ordinal: 58000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-11 (doc/review-2026-11-11/video.md V5, V7): видео не кешируется, а стримится с телефона владельца. У владельца нет ни keep-awake, ни баннера «Не блокируйте экран, идёт раздача» из architecture.md §6.3: экран гаснет, приложение приостанавливается, ТВ 15–20 с стоит и потом показывает постер. Пока играет видео, «Загрузить вообще всё» тянет до 5 T2 с каждого источника и делит эфир с видеопотоком.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Пока телефон отдаёт оригинал, приложение на нём держит экран включённым и показывает баннер о раздаче; после окончания раздачи запрет снимается
- [x] #2 Пока на экране играет видео, загрузчик показа не загружает закрепления за пределами окна; после видео загрузка продолжается
- [x] #3 Поведение покрыто тестами ядра и приложения; doc/adr и architecture.md (A64) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Ядро: отдельный meter serving (не streamed из A57) в serveBlob T3 и в ServeOriginal для своего видео; Node.Serving() = байты за 5 с; GET /serving.
2. Приложение: src/Serving.tsx — опрос GET /serving раз в 3 с, пока приложение активно; keep-awake (тег serving) и баннер, пока признак был виден за последние 60 с; подключение в App.tsx.
3. Хост: prefetch.Cache.HoldPins; сессия сообщает загрузчику «играет видео» (то же условие, что State.Playing); прогресс закреплений — reason video, текст в PinsPanel.
4. Тесты (красные отдельным коммитом), adr cache.md, architecture.md A64.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Проверка находок: V5 подтверждена (признака раздачи не было, keep-awake только на пульте). Предложение TASK-51 «тот же counted{w, &n.streamed}» не взято: streamed — скорость, отданная хостом экрану (A57), включая прокси чужого видео; добавить туда peer-T3 значило бы завысить rate_bps на хосте-владельце и считать «раздачей» прокси. Заведён отдельный meter serving (успешные T3 в serveBlob и ServeOriginal своего видео); тест проверяет, что чтение участником не меняет Node.Streamed, а хост-прокси не раздаёт.
Признак — в отдельном GET /serving: в /health нет узла (httpapi.New без узла), общего опроса в приложении нет, баннеру нужен свой; 3 с — как опрос экрана pile, только пока приложение активно.
Запас 60 с — в приложении; ядро гасит признак через 5 с.
V7 вариант 1: HoldPins в prefetch (сигнатуру Plan не менял — 56 вызовов в тестах); признак видео — то же условие, что State.Playing (session.video()), обновляется в move/control/unavailable/drive/stop.
Не проверено на устройствах: поведение iOS при автоблокировке и Android freezer, реальная выгода V7 по эфиру (замер из ревью не делался).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Ядро: отдельный счётчик раздачи своих оригиналов (peer T3 и своё видео на экран; не прокси, не смешан со скоростью A57), Node.Serving() за 5 с, GET /serving. Приложение: ServingBanner в App.tsx опрашивает раз в 3 с, держит экран и показывает баннер «Не блокируйте телефон и не сворачивайте Pileup: с него идёт видео на экран.» ещё 60 с после конца. Хост: пока играет/буферизуется видео, закрепления вне окна не планируются (HoldPins), после — догружаются; прогресс reason video. Тесты: TestServingWhileOriginalIsReadFromThisPhone, TestOwnerOfVideoOnTheScreenReportsServing, TestPinsOutsideTheWindowWaitWhileVideoPlays, app/test/serving.test.tsx, pins.test (video). go test ./..., vet, tsc, jest — зелёные. Риски: не проверено на устройствах; баннер внизу поверх экрана без safe area (TASK-45).
<!-- SECTION:FINAL_SUMMARY:END -->
