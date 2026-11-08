---
id: TASK-17
title: Показ на Google TV через Cast
status: In Progress
assignee:
  - '@claude'
created_date: '2026-11-08T06:49:00Z'
updated_date: '2026-11-08T12:33:00Z'
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

Go-часть: пакет core/cast (клиент CASTV2 для Default Media Receiver, без новых зависимостей; CastMessage кодируется вручную через encoding/binary).
- API: Connect(ctx, addr, heartbeat) → *Session (TLS без проверки сертификата, CONNECT, LAUNCH CC1AD845, CONNECT к transportId); Load(ctx, url, contentType), Play, Pause, Stop → Media{SessionID, State, IdleReason}; Media()/Changed() — последний статус и уведомление о его смене; Done()/Err(); Close().
- Ошибки: ответы receiver, кроме RECEIVER_STATUS/MEDIA_STATUS, возвращаются как *cast.Error{Type, Reason, Code(detailedErrorCode)} — LAUNCH_ERROR, LOAD_FAILED, INVALID_REQUEST и т. п. Сессия завершается с ErrClosed (обрыв), ErrNoHeartbeat (нет входящих сообщений 3 интервала heartbeat), ErrSessionEnded (CLOSE от receiver или приложение пропало из RECEIVER_STATUS), ErrNotLaunched (в ответе на LAUNCH нет приложения). Переподключение — новым Connect, автоматического нет.
- LOAD: contentId = URL, streamType BUFFERED, autoplay; по одному LOAD на элемент показа.
- Проверено против фейкового receiver (TLS на локальном порту): показ фото, затем видео, пауза/продолжение/стоп; LOAD_FAILED и INVALID_REQUEST как ошибки, сессия после них работает; LAUNCH_ERROR; PING/PONG в обе стороны держат сессию; молчащий receiver обнаруживается, ожидающий Load получает ошибку; переподключение после обрыва; уведомление о FINISHED; завершение сессии при остановке приложения.
- Остаётся на реальном Google TV: загружает ли receiver медиа по http с LAN-адреса телефона (риск 8), HEVC с iPhone, обнаружение mDNS _googlecast._tcp (нативно), реальное поведение ответа на LAUNCH (приходит ли RECEIVER_STATUS с приложением в ответ на сам запрос).

Go-клиент влит в master (24aed07), решение A14 записано. На устройстве осталось: загрузка медиа по http с LAN-адреса телефона, HEVC с iPhone, обнаружение _googlecast._tcp (нативно), приходит ли transportId в ответе на LAUNCH, playerState для изображений.
<!-- SECTION:NOTES:END -->
