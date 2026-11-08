---
id: TASK-17
title: Показ на Google TV через Cast
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:26:00Z'
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Go-часть (клиент CASTV2, пакет core/cast):
1. Кадры CASTV2: 4 байта длины big-endian + protobuf CastMessage, кодек на encoding/binary без новой зависимости.
2. Connect(addr): TLS без проверки сертификата, CONNECT к receiver-0, PING/PONG, обнаружение молчащего receiver, LAUNCH Default Media Receiver (CC1AD845), CONNECT к transportId.
3. Load(url, contentType) — один LOAD на элемент показа; Play/Pause/Stop по mediaSessionId; статус воспроизведения и уведомления об изменениях.
4. Ошибки receiver (LAUNCH_ERROR, LOAD_FAILED, INVALID_REQUEST и т. п.) возвращаются как ошибки с типом, причиной и кодом; обрыв, молчание и завершение сессии receiver — через Done/Err; переподключение — новым Connect.
5. Тесты против фейкового receiver (TLS на локальном порту): показ фото и видео, пауза/продолжение/стоп, heartbeat, мёртвый receiver, LOAD_FAILED, ошибка запуска, переподключение.
6. Проверка на реальном Google TV (http из LAN, HEVC, mDNS) — вне Go-части, остаётся на устройство.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Начата Go-часть: клиент CASTV2 для Default Media Receiver с тестами против фейкового receiver. Обнаружение (mDNS) и проверка на Google TV — позже.
<!-- SECTION:NOTES:END -->
