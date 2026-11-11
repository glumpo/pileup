---
id: TASK-41
title: Нечитаемый piles.json не мешает запуску, таймауты загрузки видны в журнале
status: Done
assignee:
  - '@claude'
created_date: '2026-11-11T00:31:00Z'
updated_date: '2026-11-11T00:31:00Z'
labels:
  - core
dependencies: []
priority: medium
type: bug
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
После TASK-28 piles.json прежнего формата или битый файл не давал узлу стартовать: приложение просто не запускалось. Таймауты загрузки T1/T2 и проксирования оригинала отменяли контекст, а report пропускает context.Canceled, поэтому молчание владельца не попадало в журнал диагностики (найдено при TASK-32/33).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Нечитаемый piles.json переименовывается в piles.json.broken-<время>, узел стартует без pile, событие в журнале
- [x] #2 Таймаут заголовков и простоя тела при загрузке T1/T2 и проксировании оригинала пишется в журнал как timeout
- [x] #3 Каждое поведение сначала воспроизведено красным тестом
- [x] #4 doc/adr обновлены
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красные тесты: TestUnreadablePilesFileIsSetAside (4 случая: Open возвращал ошибку), TestFetchTimeoutsAreLogged (нет заголовков; тело остановилось) и TestRelayTimeoutIsLogged — события timeout не было. Реализация: readPiles в node.go разбирает файл и проверяет peers всех pile; при ошибке os.Rename в piles.json.broken-<UTC> и запись в diag (sync). В download и relayOriginal context.WithCancelCause с причиной errSilent («owner went silent», оборачивает DeadlineExceeded); silent превращает отмену в ErrUnavailable+errSilent, и diag.Error относит её к timeout. Обрыв потока экраном не пишется. Проверка: go test -race ./... и make check зелёные.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Битый или прежнего формата piles.json откладывается как piles.json.broken-<время>, узел стартует без pile и пишет об этом в журнал. Таймауты загрузки T1/T2 и проксирования видео записываются в журнал как timeout «owner went silent». Тесты красные до изменения, зелёные после; doc/adr/cache.md §5.1, security.md §4.5.
<!-- SECTION:FINAL_SUMMARY:END -->
