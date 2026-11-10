# Pileup

**Pileup** — local-first приложение для совместных поездок. Фото и видео с нескольких телефонов (iPhone и Android) объединяются в одну хронологию без серверов и регистрации. Хронологию можно показать на любом экране (Apple TV, Google TV, браузер ТВ и т. д.), совместно отобрать и скачать оригиналы. Стек: ядро на Go, приложение на React Native (Expo) с тонкими нативными модулями, веб на TS. Лицензия Apache-2.0.

## Структура

- `doc/` — документация и задачи.
  - `doc/architecture.md` — требования и архитектура (`/root/prog/pileup/doc/architecture.md`). Это основной источник решений: модель данных и CRDT, протоколы экранов, кеш, безопасность, план по этапам, журнал решений.
  - `doc/adr/` — архитектурные документы по реализованному функционалу, по файлу на подсистему; описывают текущую реализацию (источник истины — код). При изменении подсистемы обновлять её документ.
  - `doc/backlog/` — задачи проекта в Backlog.md. Работать с ними только через CLI `backlog` (см. ниже). **Команды `backlog` запускать из `doc/`**: из корня репозитория CLI проект не находит.

## Правила работы с кодом

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

### 5. Do not write comments

Comments are not really needed in most cases. Do not remove existing comments, and do not add new ones unless explicitly requested.

### 6. Write tests for specification, not implementation

Do not copy or leak implementation details into the test. You must assume correct behavior first and write tests after.

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
