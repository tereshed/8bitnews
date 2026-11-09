# newsroom — конфигурация контент-команд PolyMaths

Источник правды для промптов агентов и контрактов артефактов контент-команд (см. `docs/architecture.md` §5).
Настройки в PolyMaths синхронизируются отсюда: `python3 newsroom/sync.py <team> [--dry-run]`
(ключ — env `POLYMATHS_API_KEY` или `.mcp.json`). Правки в интерфейсе PolyMaths следующий запуск перезапишет.

- `teams/<team>/team.json` — манифест команды: `team_id` (пустой — `sync.py` создаст команду и впишет id), `name`
  и `type` для создания, описание для делегирования, агенты (`model`, `code_backend`, `provider_kind`,
  `reasoning_effort` — профиль исполнения; отсутствующие поля сохраняют настройки PolyMaths;
  `settings` — настройки песочницы, например Docker; `skills` — skills проекта), контракты.
- `skills/<name>/` — файлы skill'а проекта (`SKILL.md` и env-файл с `${secret:…}`): `sync.py` создаёт или обновляет
  skill и выдаёт его ровно тем агентам, у которых он в `skills` любого `team.json`. `hub-access` — адрес и ключ hub
  для скриптов `tools/`; `studio-video` — ключ генератора картинок, только video_producer.
- `teams/<team>/<agent>.md` — промпт агента. Итоговый `system_prompt` = файл агента + `teams/<team>/_common.md`
  (кроме `router.md`, он берётся как есть).
- `tools/pm_artifact.py` — агенты скачивают по id артефакты, которые Newsroom передаёт командам
  (`docs/architecture.md` §5, «Передача данных между командами»).
- `tools/video_build.py` — вся работа Video одной командой: выбор шаблона, ведущего и трека по последним роликам
  канала (`studio pick`), сборка студией в Docker с картинками сцен, `studio validate`, загрузка пакета в хранилище;
  в stdout — `video_package`.
- `tools/video_review_kit.py` — QA и Legal скачивают ключевые кадры, обложку и субтитры ролика для просмотра.
- `tools/release_submit.py` — editor сдаёт готовый ролик в hub (`release_submit`) по id артефактов.
- `tools/source_check.py` — Research проверяет ссылки почти так же, как hub при сдаче: тот же бот, те же признаки
  заглушек, та же копия в Wayback. `bad` почти наверняка значит отказ hub, а `ok` ничего не гарантирует: у hub другой IP
  и HTTP/2. Ссылки опросов hub не проверяет, инструмент тоже.
- `tools/stage_report.py` — проверяемый разбор результатов пяти команд и привязки ревью к артефакту, без пересказа LLM.
- `automation.py schedules` — план расписаний каналов; `--apply` создаёт отсутствующие в PolyMaths.
- `automation.py status` — статусы, наличие квитанций и обнаружение зависших прогонов только на чтение.
- `tools/hublib.py` — общий клиент hub и чтение ключей из skills.
- `contracts/<kind>.schema.json` — JSON Schema (draft 2020-12) контракта артефакта. Общие части (источник, факт,
  группа мнений) повторяются в нескольких схемах, поэтому JSON генерируется: правь `gen_contracts.py` и запускай
  `python3 newsroom/gen_contracts.py`, затем `sync.py`.

## Команды

Контентные исполнители используют Claude Code (`anthropic_oauth`). Профили (решение владельца 09.11.2026, экономия токенов):
`producer` и `video_producer` запускают скрипты и работают на `claude-haiku-5-5`; те, кто собирает и пишет (`editor`,
`fact_researcher`, `local_press_analyst`, `world_press_analyst`, `analyst`, `scriptwriter`), — `claude-opus-5-5`, `medium`;
те, кто проверяет (`fact_checker`, `script_editor`, Legal, QA), — `claude-opus-5-5`, `high`. Уровень рассуждений также задан
в `settings.code_backend_settings.env.DEVTEAM_AGENT_REASONING_EFFORT` для запуска в песочнице.

Legal читает выжимки (`countries/<cc>/legal_digest.md`, `operator/legal_digest.md`, `platforms/<platform>/rules_digest.md`), а полный раздел
базы открывает, когда сработал триггер (`tools/kb_section.py`). Правишь базу — поправь и выжимку: тест `test_legal_digests.py` ловит новые и потерянные
разделы, но не изменённое правило. Сценарий после доработки проверяет один `legal_lead` по изменениям, пока `tools/script_diff.py` не потребует всю команду;
готовый ролик Legal смотрит только в том, чего в сценарии не было видно, пока `tools/legal_final_scope.py` не потребует проверить и текст.

Надёжность, очередь доработок, запуск расписаний и проверка готовности — [docs/automation.md](../docs/automation.md).

| Команда | Статус | Агенты → артефакт |
|---|---|---|
| Research | настроена | fact_researcher → `facts_report`, local_press_analyst → `local_press_report`, world_press_analyst → `world_press_report`, analyst → `research_dossier`, fact_checker (reviewer) |
| Newsroom | настроена (стадии 1–9) | editor → `story_brief` и `release_receipt`, producer → `stage_report` |
| Legal | настроена | legal_counsel → `law_report`, platform_reviewer → `platform_report`, legal_lead → `legal_review` |
| Script | настроена | scriptwriter → `script`, script_editor (reviewer) |
| Video | настроена 28.10 | video_producer (Docker) → `video_package` |
| QA | настроена 28.10 | qa_validator → `validation_report` |
