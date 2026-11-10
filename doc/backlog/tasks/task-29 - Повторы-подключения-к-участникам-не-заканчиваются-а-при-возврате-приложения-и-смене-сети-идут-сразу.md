---
id: TASK-29
title: >-
  Повторы подключения к участникам не заканчиваются, а при возврате приложения и
  смене сети идут сразу
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T17:33:00Z'
updated_date: '2026-11-10T22:56:00Z'
labels:
  - core
  - app
dependencies:
  - TASK-28
documentation:
  - adr/security.md
  - architecture-review.md
priority: high
type: bug
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Цикл подключения к адресу участника делает 100 повторов с паузой n×10 мс (около 50 с) и завершается. Это политика R14/A28. Новый цикл запускают только входящий announce, новый адрес или новое объявление mDNS. Поэтому после сетевого сбоя длиннее минуты (перезагрузка роутера, участник ушёл из Wi-Fi и вернулся) связь не восстанавливается сама, и совет «разбудите телефон и откройте Pileup» не помогает: нужен перезапуск приложения (Н1 в doc/architecture-review.md). Устаревшие адреса теперь вытесняются (TASK-28), поэтому бесконечные повторы не будут вечно стучаться в мёртвые адреса. Решение согласовано в обсуждении ревью архитектуры 2026-11-10: после линейного участка повторять раз в 30 с; при возврате приложения на передний план и при смене сети пробовать сразу.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 При сетевых и неоднозначных ошибках цикл подключения не завершается: после 100 линейных пауз (10 мс … 1 с) повторяет с паузой 30 с; ошибки подлинности, собственный адрес, вытеснение и отмена по-прежнему завершают цикл
- [x] #2 Входящий announce с адреса, чей цикл ждёт паузы, запускает попытку сразу
- [x] #3 Запрос UI на переподключение запускает сразу попытку во всех циклах, ждущих паузы, и новый цикл для сохранённых адресов без цикла; приложение отправляет его при возврате на передний план
- [x] #4 Смена сетевых адресов устройства запускает то же переподключение
- [x] #5 Переподключение и его причина пишутся в журнал диагностики
- [x] #6 Каждое поведение сначала воспроизведено красным тестом
- [x] #7 doc/adr/security.md, api/openapi.yaml и doc/architecture.md (решение) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Красные тесты: core/node (повторы без конца с паузой 30 с; входящий announce прерывает паузу; Reconnect прерывает паузы и запускает циклы сохранённых адресов без цикла; смена адресов устройства вызывает переподключение; записи журнала), core/httpapi (POST /reconnect), app (возврат на передний план вызывает POST /reconnect). Старые тесты политики R14 (исчерпание серии) переписать под новую политику.
2. connect: пауза n×10 мс для n ≤ 100, дальше 30 с, без исчерпания. Пауза прерывается: у цикла отменяемый контекст паузы; прерывание обнуляет счётчик.
3. Node.Reconnect(reason): прервать паузы всех циклов, запустить циклы для сохранённых адресов без цикла, записать в журнал. Входящий announce прерывает паузу своего адреса.
4. Слежение за сетью: раз в 5 с сравнивать Config.Addrs; при изменении — Reconnect.
5. httpapi POST /reconnect (204), openapi.yaml; app: AppState active → POST /reconnect.
6. Документы: security.md §5.4, A36 (заменяет политику повторов A28), Н1 в architecture-review.md. gofmt, vet, -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красные тесты на старом коде (заглушки Reconnect и networkCheck без логики):
- TestAmbiguousFailuresAreRetriedWithoutEnd: «101 connection attempts» — серия кончалась после 100 повторов.
- TestSeriesRestartsOnlyAfterSuccessfulSync: «151 attempts» — после удачного раунда тоже ровно 100 повторов и конец.
- TestMemberIsRetriedAfterLongAbsence: «a long pause after the linear retries: timed out» — паузы 30 с не было, цикл заканчивался.
- TestIncomingAnnounceCutsThePause, TestReconnectRetriesNow (оба случая), TestNetworkChangeReconnects: timed out — цикл в паузе не просыпался.
- core/httpapi TestReconnectAsksNodeToRetryNow: «status = 404, want 204».
- app test/reconnect.test.tsx: при переходе в active запроса POST /reconnect не было.
- TestFoundAfterLostStartsNewLoop зелёный и на старом коде: защищает поведение mDNS (повторный Found не создаёт второй цикл, Found после Lost — создаёт) вместо удалённого TestOnlyNewDiscoveryStartsNewSeries, который проверял конец серии.
Удалён TestReturningMemberIsSyncedAfterSeriesEnded: его предпосылка (серия закончилась) больше не существует; возвращение участника после долгого отсутствия проверяет TestMemberIsRetriedAfterLongAbsence. Тестовый тормоз runaway снижен с 500 до 160 пауз: этого хватает, чтобы увидеть паузы 30 с после 100 линейных, и тесты стали в 2–3 раза быстрее.

Реализация (core/node/reconnect.go): delay(n) = n×10 мс для n ≤ 100, дальше 30 с; исчерпания нет. Пауза идёт на дочернем контексте; его отмена (kick) прерывает паузу, попытка идёт сразу, счётчик обнуляется. Отмена самого цикла и ошибка тестового хука паузы по-прежнему завершают цикл. hurry(addr): нет цикла — запустить, цикл в паузе — прервать; вызывается из входящего announce вместо dial. Node.Reconnect(reason): прервать паузы всех циклов, запустить циклы для адресов из peers без цикла, записать в журнал («reconnect, <причина>: N waiting loops retried now, M loops started»). watchNetwork: раз в 5 с сравнивает Config.Addrs, при изменении пишет «network addresses changed from … to …» и вызывает Reconnect. httpapi: POST /reconnect → 204 (api/openapi.yaml). app: App.tsx при AppState active вызывает api.reconnect().

Проверка: gofmt, go vet, go test -race ./..., 10 прогонов под -race тестов повторов и адресов, make check — exit 0 (46 тестов приложения).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Повторы подключения к адресу участника больше не заканчиваются: после 100 линейных пауз (10 мс … 1 с) попытка раз в 30 с; цикл завершают только ошибки подлинности, собственный адрес, вытеснение, Lost кандидата и закрытие узла. Паузу прерывают входящий announce с этого адреса, возврат приложения на передний план (POST /reconnect из App.tsx) и смена сетевых адресов устройства (проверка раз в 5 с); Reconnect также запускает циклы для сохранённых адресов без цикла. Переподключение и его причина пишутся в журнал. Решение A36 (заменяет политику R14/A28); обновлены doc/adr/security.md §4.1, §4.2, §5.4, F7, F8, §8, api/openapi.yaml, Н1 в doc/architecture-review.md.
Проверка: каждое поведение сначала красным тестом, затем зелёным; go test -race ./..., 10 прогонов под -race, make check — зелёные.
Риски: недоступный участник опрашивается раз в 30 с всё время, пока приложение открыто; один mDNS-кандидат, который никогда не ответит, тоже опрашивается до Lost.
<!-- SECTION:FINAL_SUMMARY:END -->
