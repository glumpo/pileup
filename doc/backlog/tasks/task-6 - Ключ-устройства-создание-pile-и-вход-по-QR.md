---
id: TASK-6
title: Ключ устройства, создание pile и вход по QR
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:28:00Z'
labels:
  - core
  - app
  - protocol
milestone: m-0
dependencies:
  - TASK-3
  - TASK-4
documentation:
  - architecture.md
priority: medium
type: feature
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Регистрации нет: участник определяется ключом устройства, а членство в pile — знанием секрета из QR-приглашения (architecture.md, раздел 7). Ключ не должен уходить в бэкап, иначе восстановленное приложение продолжит чужой журнал и создаст форк.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Ключ ed25519 создаётся при первом запуске и хранится в Keychain и Keystore без переноса в бэкап
- [ ] #2 Создатель pile показывает QR по кнопке; QR содержит pile_id, секрет pile и адреса создателя
- [ ] #3 Сканирование QR на другом телефоне создаёт операцию Join, proof которой проверяется у всех участников, в том числе у тех, кто не присутствовал при входе
- [ ] #4 Работает в обе стороны: iPhone приглашает Android и Android приглашает iPhone
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
План покрывает только Go-часть (нативное хранение ключа в Keychain/Keystore и экран QR — вне этого плана).
1. core/device: интерфейс хранилища ключа (Load/Save seed), Key() — создать ed25519 при первом использовании и загружать потом; файловая реализация для десктопа и тестов → тест: ключ создаётся один раз и перезагружается, новое хранилище — новый автор.
2. core/invite: компактный версионированный формат приглашения для QR (pile_id, секрет, адреса пригласившего) → тесты: round-trip, отказ на битых строках и неизвестной версии.
3. core/node: узел устройства — ключ, список pile с секретами и пирами в каталоге данных, создание pile (Join, затем PileCreate), приглашение, вход по приглашению (проверка секрета подключением к пригласившему, Join, синхронизация); один listener на pile поверх core/replica; тянущая сторона сообщает свой порт, и пригласивший начинает тянуть у новичка → интеграционные тесты на трёх узлах через loopback, поддельное приглашение, обе стороны как пригласивший, переживание перезапуска.
4. core/httpapi + api/openapi.yaml: создание pile, список, приглашение, вход по приглашению под токеном → HTTP-тесты.
5. gofmt, go vet, go test -race, make check.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: формат приглашения, создание pile и вход в ядре, HTTP API для UI. Нативное хранение ключа (Keychain/Keystore) и экран QR — позже.
<!-- SECTION:NOTES:END -->
