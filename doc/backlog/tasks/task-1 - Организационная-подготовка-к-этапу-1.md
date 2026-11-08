---
id: TASK-1
title: Организационная подготовка к этапу 1
status: To Do
assignee: []
created_date: '2026-11-08T06:48:00Z'
labels:
  - org
milestone: m-0
dependencies: []
documentation:
  - architecture.md
priority: high
type: chore
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Без аккаунта Apple Developer нельзя ставить сборки на iPhone, а без Mac нельзя собрать iOS. Multicast-entitlement понадобится только на этапе 2 (DLNA), но Apple отвечает долго, поэтому заявку подаём в начале этапа 1. Для проверки этапа нужен фиксированный набор реальных устройств.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Аккаунт Apple Developer активен, сборка ставится на тестовый iPhone
- [ ] #2 Есть Mac с Xcode для сборки iOS
- [ ] #3 Заявка на entitlement com.apple.developer.networking.multicast подана, дата подачи записана в задаче
- [ ] #4 Доступны тестовые устройства: iPhone, Android-телефон, Apple TV, Google TV и Wi-Fi-роутер, модели и версии ОС записаны в задаче
<!-- AC:END -->
