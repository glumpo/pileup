---
id: TASK-54
title: >-
  Журнал диагностики переживает перезапуск и падение; в него попадают ошибки JS
  и нативного слоя
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-11T01:54:00Z'
updated_date: '2026-11-11T11:45:00Z'
labels:
  - core
  - app
  - review
dependencies:
  - TASK-46
priority: high
type: enhancement
ordinal: 54000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ревью 2026-11-11 (doc/review-2026-11-11/diagnosability.md D3, D15, D16): журнал живёт только в памяти (A23) — после падения или перезапуска, когда он нужнее всего, он пуст. Паники Go, ошибки JS и нативного слоя следов не оставляют; Android-модуль глотает ошибки NSD. Записи не самодостаточны: hex-ключи вместо имён, у экспорта нет версии приложения, платформы и состояния участников.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 События журнала сохраняются на диск с ограничением размера и видны после перезапуска ядра; запуск отмечается записью с версией и платформой
- [x] #2 Аварийное завершение Go-ядра оставляет след, который виден в журнале после следующего запуска
- [ ] #3 Необработанные ошибки JS и ошибки нативных провайдеров (обнаружение, медиатека) попадают в журнал
- [x] #4 Экспорт журнала начинается с версии приложения, платформы и состояния участников; в записях имена участников рядом с ключами
- [x] #5 Невозможность записи журнала (нет места) не ломает работу ядра
- [x] #6 Поведение покрыто тестами; doc/adr и architecture.md (A60) обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. core/diag: diag.Open(dir, size) — журнал дописывается в diag.log строками JSON, ротация 256 КиБ в diag.log.1; при открытии кольцо восстанавливается из файлов. Повтор серии пишется в файл не чаще раза в минуту. Ошибка записи — одно событие в памяти, повтор открытия раз в минуту. Строки приглашения вырезаются из сообщений. Новые виды app и crash. Close.
2. core/node: журнал создаётся diag.Open в cfg.Dir, запись «Pileup <версия> <платформа> started»; Close закрывает журнал.
3. core/mobile: debug.SetCrashOutput в crash.txt, при следующем Start непустой crash.txt — событие crash в журнале. mobile.Report(kind, message string) — в любой момент, до Start и после Stop копит до 100 событий. Токен вырезается.
4. core/httpapi: POST /diag {kind, message}; экспорт с заголовком (версия, платформа, свой ключ, время, pile с участниками и их состоянием); в peer hex-ключ участника → «имя (8 hex)» в GET /diag и экспорте.
5. app: src/report.ts — ErrorUtils.setGlobalHandler, необработанные отклонения Hermes, Boundary.componentDidCatch → POST /diag; приёмник задаётся в connect(). Log.tsx: подписи новых видов.
6. Kotlin: NsdDiscovery и MediaStoreProvider → Mobile.report (не скомпилировано).
7. Тесты сначала красные; doc/adr/diag.md, security.md, A60.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Сделано по варианту 1 из D3. Склейка: в памяти как раньше; в файл — первое событие серии, затем не чаще раза в минуту, остаток счётчика при Close. Повторные запуски одной версии склеиваются в одно событие app (время — последнего запуска). Формат файла — строки JSON. Медиатека: ошибки, которые проходят через Go (Media, Rendition), уже попадают в журнал через HTTP API (TASK-47) и report рендера; в Kotlin молча терялся только пустой курсор MediaStore — он теперь идёт в Mobile.report. Отказ разрешений в журнал не пишется (D7, вне объёма). Kotlin (NsdDiscovery.kt, MediaStoreProvider.kt) не скомпилирован локально — AC#3 в части натива проверяется сборкой; Go-сторона mobile.Report покрыта тестами. Влит origin/master (TASK-48, TASK-49), конфликт в node.Open разрешён.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Журнал диагностики хранится на диске (diag.log + diag.log.1 по 256 КиБ, строки JSON) и восстанавливается при запуске; запуск отмечается версией и платформой. mobile.Start включает debug.SetCrashOutput в crash.txt, след паники Go появляется событием crash после следующего запуска. Вход для чужих ошибок: POST /diag (глобальный обработчик ErrorUtils, Hermes rejections в release, Boundary) и mobile.Report(kind, message string) — в любой момент, до Start/после Stop копит до 100. Строки приглашения и UI-токен вырезаются. Экспорт начинается с версии, платформы, своего ключа и участников pile с состоянием; hex-ключи участников в peer заменены на «имя (8 hex)». Ошибка записи журнала — событие в памяти и повтор через минуту. Тесты: diag (persist, /dev/full), node, httpapi, mobile (дочерний процесс паникует), jest report.test. doc/adr/diag.md, security.md, A60. Kotlin не скомпилирован.
<!-- SECTION:FINAL_SUMMARY:END -->
