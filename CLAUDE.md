# Pileup

**Pileup** — local-first приложение для совместных поездок. Фото и видео с нескольких телефонов (iPhone и Android) объединяются в одну хронологию без серверов и регистрации. Хронологию можно показать на любом экране (Apple TV, Google TV, браузер ТВ и т. д.), совместно отобрать и скачать оригиналы. Стек: ядро на Go, приложение на React Native (Expo) с тонкими нативными модулями, веб на TS. Лицензия Apache-2.0.

## Структура

- `doc/` — документация и задачи.
  - `doc/architecture.md` — требования и архитектура (`/root/prog/pileup/doc/architecture.md`). Это основной источник решений: модель данных и CRDT, протоколы экранов, кеш, безопасность, план по этапам, журнал решений.
  - `doc/backlog/` — задачи проекта в Backlog.md. Работать с ними только через CLI `backlog` (см. ниже). **Команды `backlog` запускать из `doc/`**: из корня репозитория CLI проект не находит.

<!-- BACKLOG.MD GUIDELINES START -->
<!-- backlog.md-instructions-version: 1.55.3 -->
<CRITICAL_INSTRUCTION>

## Backlog.md Workflow

This project uses Backlog.md for task and project management.

**At the beginning of each conversation in this project, run `backlog instructions overview` before answering or taking action. Re-read it only if you have not read it yet in the current conversation.**

Use the overview to decide whether to search, read, create, or update Backlog tasks.

Before task lifecycle actions, read the matching detailed guide:
- `backlog instructions task-creation` before creating or splitting tasks
- `backlog instructions task-execution` before planning, changing status or assignee, adding a plan or implementation notes, or implementing task work
- `backlog instructions task-finalization` before checking acceptance criteria, writing final summaries, or moving tasks to terminal statuses

Use `backlog <command> --help` before running unfamiliar commands. Help shows options, fields, and examples.

Do not edit Backlog task, draft, document, decision, or milestone markdown files directly. Use the `backlog` CLI so metadata, relationships, and history stay consistent.

</CRITICAL_INSTRUCTION>
<!-- BACKLOG.MD GUIDELINES END -->
