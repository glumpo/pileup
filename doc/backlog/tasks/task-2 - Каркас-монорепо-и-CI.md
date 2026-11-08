---
id: TASK-2
title: Каркас монорепо и CI
status: Done
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T10:13:00Z'
labels:
  - core
  - app
  - ci
milestone: m-0
dependencies: []
documentation:
  - architecture.md
priority: high
type: task
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Всё дальнейшее строится в одном репозитории: Go-ядро, приложение на React Native (Expo), схема API. Проект открытый под Apache-2.0, и GPL-зависимости недопустимы из-за несовместимости с App Store, поэтому проверка лицензий нужна с первого коммита, а не задним числом.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Репозиторий содержит каталоги core/ (Go-модуль), app/ (Expo-приложение), api/ (схема API)
- [x] #2 В корне лежит LICENSE с текстом Apache-2.0
- [x] #3 CI на каждый push запускает go test и сборку ядра
- [x] #4 CI проверяет лицензии Go- и npm-зависимостей и падает на GPL, AGPL и LGPL
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. LICENSE (Apache-2.0) и корневой .gitignore → проверка: текст совпадает с apache.org
2. core/: Go-модуль github.com/glumpo/pileup/core, пакет httpapi с обработчиком GET /health и проверкой bearer-токена, тесты → go test ./... и go build ./...
3. api/openapi.yaml (OpenAPI 3.1): GET /health, bearer, ответы 200 и 401 → валидация схемы
4. app/: create-expo-app с шаблоном blank-typescript → npx tsc --noEmit
5. Проверка лицензий: go-licenses для core, license-checker-rseidelsohn для prod-зависимостей app, запрет GPL/AGPL/LGPL → проходит на реальных зависимостях, падает на временно добавленной GPL-зависимости
6. Makefile в корне: make check запускает тесты, сборку и проверку лицензий
7. .github/workflows/ci.yml на push, вызывает те же цели make → actionlint
8. Прогон make check с чистого клона
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Каркас создан: LICENSE, .gitignore, core/ (httpapi с /health), api/openapi.yaml, app/ (create-expo-app, blank-typescript).
make check проходит. Проверка лицензий: go-licenses (forbidden, restricted, unknown) и license-checker-rseidelsohn --production с фильтром scripts/check-npm-licenses.mjs (GPL/AGPL/LGPL/UNKNOWN; выражение с OR проходит, если есть разрешённая альтернатива). Падение проверено на временно добавленных GPL/AGPL/LGPL-зависимостях в Go и npm.
В prod-зависимостях шаблона Expo есть node-forge с лицензией (BSD-3-Clause OR GPL-2.0): проходит как двойная лицензия, требует решения владельца.

Workflow проверен actionlint (без ошибок) и прогнан локально через act (catthehacker/ubuntu:act-latest): все шаги, включая make check, успешны. make check проходит и с чистого клона.

Проверка владельцем: make check на master проходит (exit 0, ~43 с). Решения A1–A3 записаны в architecture.md, раздел «Автоматически принятые решения».
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Создан каркас монорепо: core/ (Go-модуль github.com/glumpo/pileup/core с обработчиком GET /health и тестами), app/ (Expo blank-typescript), api/openapi.yaml (OpenAPI 3.1, /health с bearer-токеном), LICENSE Apache-2.0, .gitignore. make check запускает go test, сборку ядра и проверку лицензий Go (go-licenses) и npm (license-checker + scripts/check-npm-licenses.mjs, отказ на GPL/AGPL/LGPL/UNKNOWN). GitHub Actions (.github/workflows/ci.yml) на каждый push выполняет make check; локально прогнан через act. Отказ проверки лицензий продемонстрирован временными пакетами с GPL/AGPL/LGPL (не закоммичены). Решения на ревью: A1–A3.
<!-- SECTION:FINAL_SUMMARY:END -->
