---
id: TASK-17
title: Показ на Google TV через Cast
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:23:00Z'
labels:
  - core
  - cast
  - show
milestone: m-0
dependencies:
  - TASK-14
documentation:
  - architecture.md
priority: high
type: spike
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
На Android TV и Google TV обычно нет браузера, поэтому основной путь к ним — Google Cast. Используем Default Media Receiver без регистрации и своего receiver: хост по CASTV2 из Go отправляет URL медиа, которые сам раздаёт по http. Главный риск — что receiver загружает медиа по http с LAN-адреса телефона; если нет, понадобится свой receiver с HTTPS.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Хост находит Cast-устройства через mDNS (_googlecast._tcp) на iPhone и на Android
- [ ] #2 С iPhone-хоста и с Android-хоста фото с обоих источников показываются на Google TV, листание с телефона
- [ ] #3 Видео с обоих источников проигрывается на Google TV, включая HEVC с iPhone; результат по кодекам записан в задаче
- [ ] #4 Если receiver не загружает медиа по http из LAN, это зафиксировано в задаче вместе с вариантами обхода
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: клиент CASTV2 для Default Media Receiver с тестами против фейкового receiver. Обнаружение (mDNS) и проверка на Google TV — позже.
<!-- SECTION:NOTES:END -->
