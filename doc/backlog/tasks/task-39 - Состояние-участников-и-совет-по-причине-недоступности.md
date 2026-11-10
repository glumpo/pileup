---
id: TASK-39
title: Состояние участников и совет по причине недоступности
status: Done
assignee:
  - '@claude'
created_date: '2026-11-10T22:58:00Z'
updated_date: '2026-11-10T23:46:00Z'
labels:
  - core
  - app
dependencies: []
documentation:
  - architecture-review.md
  - adr/security.md
priority: high
type: enhancement
ordinal: 39000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Причины недоступности видны только на пульте во время показа, и совет один на все случаи: «разбудите телефон и откройте Pileup». Вне показа (экран pile, сетка, просмотр) состояния участников не видно совсем. Ядро различает больше: адрес неизвестен с запуска, соединение отвергнуто (приложение закрыто), тайм-аут (спит или занят), нет маршрута (ушёл из сети), на адресе другое устройство, время последней связи. Текст load_failed на пульте («Попробуйте следующий») выводится во время автоматического повтора, хотя причина чаще в недоступном владельце. Н9 в doc/architecture-review.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 GET /piles отдаёт по каждому участнику его ключ, на связи ли он, когда был на связи последний раз и класс последней ошибки подключения
- [x] #2 Экран pile показывает состояние каждого участника и совет по классу ошибки
- [x] #3 Подсказки пульта используют тот же совет; при недоступном владельце текущего файла вместо load_failed показывается подсказка об этом владельце
- [x] #4 Каждое поведение сначала воспроизведено красным тестом
- [x] #5 api/openapi.yaml и затронутые doc/adr обновлены
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Ядро, node: по адресу участника хранить класс последней ошибки цикла подключения (errors.Is/As: ECONNREFUSED → refused; net.Error.Timeout → timeout; EHOSTUNREACH/ENETUNREACH → no_route; ErrMembership/ErrCertificate/ErrPeerChanged → other_device; прочее → error), очищать при подключении, удалять вместе с адресом; по участнику — время конца последнего соединения. Node.Presence(pile, участник) → {Online, LastSeen, LastError}: на связи — без ошибки и LastSeen = сейчас; иначе лучший класс по адресам (refused, timeout, no_route, other_device, error, ещё нет ошибки), без адресов — no_address; своё устройство всегда на связи. Красные тесты в node (закрыто → refused, тайм-аут, другое устройство, без адреса, свёртка по адресам, время).
2. httpapi: GET /piles отдаёт у участника key, online, last_seen (мс или null), last_error. Красный тест через UI API.
3. show: Hint получает LastError участника (JSON last_error); тест подсказки обновить.
4. Приложение: общий совет по классу (src/presence.ts); экран pile показывает состояние каждого участника с советом и обновляет его опросом GET /piles; пульт подбирает совет по last_error; load_failed при подсказке о владельце текущего файла не показывается. Красные тесты RNTL.
5. api/openapi.yaml, doc/adr/security.md (§4.5/§5.4), doc/adr/cache.md (интерфейсы, подсказки).
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Красные тесты (ec02ca0), все падали на поведении:
- node: TestMemberIsOnlineUntilItsPileupCloses — «connected member: {Online:false LastSeen:0001-01-01 00:00:00 +0000 UTC LastError:}, want online now without an error»; TestLastConnectionErrorOfMemberIsClassified (refused, timeout, no_route, other_device ×2, error) и TestMostHopefulErrorOfMemberAddressesIsReported (4 пары классов) — «the member's error is …: timed out»; TestMemberWithoutAddressAndThisDevice — «member without a known address: {… LastError:}», «this device: {Online:false …}, want online now».
- httpapi: TestPileListsWhetherMembersAreConnected — «members map[Аня:{Key: Name:Аня Online:false LastSeen:<nil> LastError:} …], want Аня with key … and both online».
- show: TestHintNamesMemberWhoseFilesWait и TestVideoOfUnavailableSourceShowsPosterAndShowGoesOn ждут подсказку с LastError refused — «hint names c: timed out», «video unavailable with a hint: timed out».
- app: members.test.tsx — «Unable to find an element with text: Аня — на связи» (и «Боря — на связи» для обновления опросом); piles/join — вместо строки «Участники: …» ждут состояния; show.test.tsx «members the show waits for are advised…» — «Unable to find … Нет ответа от iPhone: Аня. Pileup закрыт — попросите открыть Pileup.» (был один совет на все случаи); «a file the screen could not open while its owner does not answer…» — на экране «Экран не смог открыть этот файл. Попробуйте следующий.».

Реализация:
- core/node/presence.go: классы через errors.Is/As — refused (ECONNREFUSED), timeout (net.Error.Timeout()), no_route (EHOSTUNREACH, ENETUNREACH), other_device (ErrMembership, ErrCertificate, ErrPeerChanged), error (прочее, в том числе EOF), no_address (нет адресов). connect после каждой неудачной попытки (кроме отмены контекста) пишет класс в p.faults[адрес], если адрес в peers; learn стирает класс адреса, drop удаляет его с адресом; left запоминает p.seen[участник]. Node.Presence(pile, участник): своё устройство или живое соединение — Online, LastSeen = сейчас, без класса; иначе LastSeen = конец последнего соединения (нулевое — не было с запуска) и лучший класс по адресам в порядке refused, timeout, no_route, other_device, error, «ещё нет неудачи», no_address. Для теста тайм-аута announce вынесен в var announceTimeout (SetAnnounceTimeout в retry_export_test.go).
- httpapi: у участника key, online, last_seen (мс или null), last_error (omitempty); toJSON(n, p) во всех трёх маршрутах. Подсказка показа: last_error из Presence (show.Hint.LastError).
- app: src/presence.ts — adviceFor(класс) и presenceOf(участник); экран pile: заголовок «Участники» и строка на участника («Аня — на связи», «Боря — нет связи с 14:05. Pileup закрыт — попросите открыть Pileup.», «… с запуска приложения …»), опрос GET /piles раз в 3 с. Пульт: hintText берёт adviceFor(last_error) (для timeout и без класса — прежний совет); load_failed не показывается, если автор текущего файла есть в hints.
- Почему порядок refused > timeout > no_route > other_device > error: чем ближе ответ к живому Pileup, тем полезнее совет; порядок детерминирован и не мигает, в отличие от «последней ошибки» по нескольким циклам.
- Своё устройство «на связи» — чтобы экран pile не писал про себя «адрес неизвестен»; Online() по-прежнему себя не включает.
- Список «Загрузка на это устройство» по-прежнему строится по хронологии (A27), ключ участника в GET /piles используется как key строки и для сопоставления с author; менять этот список задача не просила.

Ограничения:
- no_route проверяется подключением к 255.255.255.255 (Linux сразу отвечает ENETUNREACH); EHOSTUNREACH и ErrPeerChanged отдельными тестами не воспроизведены (нужен хост без маршрута или подмена устройства за прокси), их отображение — тем же errors.Is.
- Свёртка эвристическая: старый адрес, занятый устройством без Pileup (refused), перебивает тайм-аут настоящего адреса спящего телефона; оба совета ведут к «открыть Pileup».
- Пока первые попытки не закончились (announce до 20 с), класса нет: экран показывает общий совет.
- last_seen и классы только в памяти: после перезапуска — «нет связи с запуска приложения»; время без даты.
- EOF (нам отказали: неверный секрет или другой pile на том же порту) остаётся классом error: его не отличить от обрыва во время синхронизации.
- Опрос GET /piles раз в 3 с на экране pile читает проекцию каждый раз (Н11).
- Полноэкранный просмотр (Н5) состояние участника пока не показывает.
- Наблюдение: show TestViewerCacheAccountingSurvivesRestart один раз из нескольких полных прогонов пакета упал «viewer got status 502 for P2»: просмотр сразу после перезапуска, адрес владельца ещё не подтверждён (A34). Повторные прогоны и база (3 прогона) проходят; гонка в самом тесте, к задаче не относится.

Проверка: core — gofmt -l пусто, go vet ./... чисто, go test -race -count=1 ./... — все 19 пакетов ok (node 113 с, show 96 с, httpapi 13 с). app — npx tsc --noEmit без ошибок, npx jest --ci: 9 наборов, 50 тестов, все прошли (два полных прогона подряд; один ранний прогон дал разовый сбой «found screens are listed…» в ScreenPicker — отдельно и повторно проходит, с задачей не связан).

Решение для architecture.md: Узел хранит по адресу участника класс последней неудачи цикла подключения, а по участнику — время конца последнего соединения; оба только в памяти. Классы определяются через errors.Is/As: refused (ECONNREFUSED — Pileup закрыт), timeout (net.Error с Timeout — телефон спит или занят), no_route (EHOSTUNREACH, ENETUNREACH — не в этой сети), other_device (ErrMembership, ErrCertificate, ErrPeerChanged — на адресе другое устройство или другой pile), error (остальное, в том числе EOF), no_address (адресов нет). Подключение по адресу стирает его класс, вытеснение удаляет вместе с адресом. Node.Presence: участник на связи — без класса, last_seen = сейчас; иначе last_seen — конец последнего соединения (нет — не было с запуска) и первый класс по его адресам в порядке refused, timeout, no_route, other_device, error. Своё устройство для себя всегда на связи. GET /piles отдаёт у участника key (hex ключа, как author), online, last_seen, last_error; подсказки показа — last_error. Приложение подбирает совет по классу на экране pile (строка на участника, опрос раз в 3 с) и на пульте; при подсказке о владельце текущего файла load_failed не показывается. Ключ участника в GET /piles снимает оговорку A27 «в Pile.members нет ключа автора»; список загрузки по участникам по-прежнему строится по хронологии. Дополняет A26, A27, A36 | Причины были видны только на пульте, с одним советом на все случаи, а «Попробуйте следующий» выводился, пока показ сам повторял загрузку недоступного владельца (Н9 в architecture-review.md). Порядок классов детерминирован: совет не мигает между адресами и выбирает ответ, ближайший к живому Pileup | TASK-39
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Ядро помнит по адресу участника класс последней ошибки подключения (refused, timeout, no_route, other_device, error; no_address без адресов) и время конца последнего соединения; Node.Presence сворачивает их по участнику (на связи — без ошибки; иначе лучший класс по адресам). GET /piles отдаёт у участника key, online, last_seen, last_error, подсказки показа — last_error. Экран pile показывает каждого участника («на связи» / «нет связи с 14:05» + совет по классу) и обновляет состояние опросом раз в 3 с; пульт использует тот же совет и не выводит load_failed, когда владелец текущего файла в подсказках. Обновлены api/openapi.yaml, doc/adr/security.md §5.4, doc/adr/cache.md. Проверка: go test -race ./... — ok; tsc, jest 50/50. Ограничения: EHOSTUNREACH и ErrPeerChanged отдельно не воспроизведены; свёртка эвристическая; состояние только в памяти; полноэкранный просмотр (Н5) состояние не показывает.
<!-- SECTION:FINAL_SUMMARY:END -->
