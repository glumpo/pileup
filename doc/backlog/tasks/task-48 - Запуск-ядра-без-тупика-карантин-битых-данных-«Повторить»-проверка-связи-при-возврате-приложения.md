---
id: TASK-48
title: >-
  Запуск ядра без тупика: карантин битых данных, «Повторить», проверка связи при
  возврате приложения
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:41:00Z'
updated_date: '2026-11-11T02:02:00Z'
labels:
  - core
  - app
  - review
dependencies:
  - TASK-46
priority: high
type: bug
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-11 (doc/review-2026-11-11/reliability.md R2-01, R2-03, R2-10; diagnosability.md D4): любая ошибка открытия ядра — тупик. Битый cache.db (это только кеш), одна битая БД pile или полный диск блокируют всё приложение: на экране сырой английский текст исключения без кнопок, перезапуск не помогает. Циклы Accept порта 127.0.0.1 не перезапускаются, а /health приложение не использует: на iOS после приостановки возможна мёртвая связь UI с ядром, из которой выходят только убийством приложения. Ошибка рендера вне Boundary роняет release-сборку; паника в обработчике под мьютексом без defer превращается в вечную блокировку.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Повреждённый cache.db откладывается в сторону и создаётся заново; узел стартует, событие в журнале
- [x] #2 Pile, БД которого не открылась, не мешает запуску и остальным pile: он виден в списке с причиной; остальные работают
- [x] #3 При сбое запуска экран называет причину по-русски (отдельно — нехватку места), даёт «Повторить» и «Поделиться подробностями»; повтор запускает ядро без перезапуска приложения
- [x] #4 При возврате приложения на передний план связь с ядром проверяется; если ядро не отвечает, оно перезапускается и приложение продолжает работу с новым портом и токеном без действий пользователя
- [x] #5 Ошибка рендера любого экрана не роняет приложение: корневая граница с текстом и возвратом к списку pile; мьютексы blob.Get и show.Host.do освобождаются при панике
- [x] #6 Каждое изменение поведения сначала воспроизведено красным тестом; doc/adr и architecture.md (A54) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Красные тесты ядра: битый cache.db откладывается (node); pile с битой БД не валит узел, виден в списке с причиной, остальные работают, piles.json сохраняет запись (node); все маршруты неоткрытого pile отвечают pile_unavailable (httpapi); ошибка ключа устройства опознаваема и seed не перезаписан (node); mobile.Start повторяем после неудачного Start; паника под мьютексом blob.Get и show.Host.do не блокирует.
2. Исправление ядра: Node.broken (entry+причина: corrupt/no_space/failed), find -> ErrBrokenPile, save не фатален при Open, имя pile в piles.json для показа неоткрытого; cache.db -> cache.db.broken-*; defer в blob.Get и show.Host.do.
3. Красные тесты приложения: экран сбоя запуска (текст, отдельно нехватка места, Повторить, Поделиться подробностями); /health при возврате и перезапуск ядра с новым портом/токеном; неоткрытый pile в списке; корневая граница ошибок с возвратом к списку.
4. Исправление приложения: App.tsx (StartFailure, restart, Boundary), api.health, Piles.
5. doc/adr, architecture.md A54, полный прогон.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Находки проверены: R2-01 воспроизведено красными тестами (битый cache.db и одна битая БД pile — Open падает с 'file is not a database (26)'); R2-10 — паника под мьютексом blob.Get и show.Host.do вешала следующие вызовы (тесты висли до таймаута). mobile.Start после неудачного Start уже повторяем — тест TestStartAgainAfterFailedStart зелёный сразу и закрепляет поведение; после Stop — существующий TestStartAgainAfterStopUsesNewToken.
Решения: неоткрытый pile — отдельный список Node.broken (entry + причина), find -> ErrBrokenPile -> 503 pile_unavailable; запись остаётся в piles.json, файлы не трогаются. Чтобы показать, какой pile не открылся, в piles.json добавлено поле name (последнее известное имя, обновляется при открытии pile). «Повторить» у неоткрытого pile (кроме corrupt) перезапускает ядро тем же путём, что и проверка связи. Ошибка ключа устройства помечается 'device key' в node.Open, ключ не пересоздаётся.
Побочный эффект: после откладывания cache.db сверка удаляет файлы blobs/<hash> (это кеш), закрепления остаются только в отложенной копии.
Не проверено: полный диск на реальном устройстве (классификация no_space по ENOSPC/SQLITE_FULL/IOERR_SHMSIZE проверена только через JS-текст), поведение iOS при приостановке. В red-коммите startup.test.tsx имел ошибку типов (Share mock), исправлена в коммите исправления.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Ядро: битый cache.db -> cache.db.broken-* и новый кеш, событие в журнале; pile с неоткрывшейся БД не валит узел, виден в GET /piles с broken (corrupt/no_space/failed) и последним именем, его маршруты — 503 pile_unavailable; save piles.json при открытии не фатален; ошибка ключа устройства помечена 'device key'; defer для мьютексов blob.Get и show.Host.do. Приложение: экран сбоя запуска (причина, отдельно нехватка места и ключ; «Повторить», «Поделиться подробностями»); при возврате на передний план GET /health (2 с), при неудаче stop/start и новый Api; неоткрытый pile в списке с причиной и «Повторить»; корневая граница ошибок с «К списку pile». Тесты: node TestCorruptCacheIsSetAsideAndRecreated, TestPileWithUnreadableDatabaseDoesNotStopOthers, TestUnreadableDeviceKeyIsNamedAndKept; httpapi TestEveryRouteOfUnopenedPileAnswersPileUnavailable; blob/show TestPanic*; mobile TestStartAgainAfterFailedStart; jest startup.test.tsx, boundary.test.tsx. Риски: идущий показ теряется при перезапуске ядра (отдельная задача); на iOS не проверено на устройстве.
<!-- SECTION:FINAL_SUMMARY:END -->
