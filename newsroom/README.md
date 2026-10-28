# newsroom — конфигурация контент-команд PolyMaths

Источник правды для промптов агентов и контрактов артефактов контент-команд (см. `docs/architecture.md` §5).
Настройки в PolyMaths синхронизируются отсюда: `python3 newsroom/sync.py <team> [--dry-run]`
(ключ — env `POLYMATHS_API_KEY` или `.mcp.json`). Правки в интерфейсе PolyMaths следующий запуск перезапишет.

- `teams/<team>/team.json` — манифест команды: `team_id` (пустой — `sync.py` создаст команду и впишет id), `name`
  и `type` для создания, описание для делегирования, агенты (`model` — если модель агента задаём сами, иначе остаётся
  модель PolyMaths; `settings` — настройки песочницы, например Docker; `skills` — skills проекта), контракты.
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
- `tools/hublib.py` — общий клиент hub и чтение ключей из skills.
- `contracts/<kind>.schema.json` — JSON Schema (draft 2020-12) контракта артефакта. Общие части (источник, факт,
  группа мнений) повторяются в нескольких схемах, поэтому JSON генерируется: правь `gen_contracts.py` и запускай
  `python3 newsroom/gen_contracts.py`, затем `sync.py`.

## Команды

| Команда | Статус | Агенты → артефакт |
|---|---|---|
| Research | настроена | fact_researcher → `facts_report`, local_press_analyst → `local_press_report`, world_press_analyst → `world_press_report`, analyst → `research_dossier`, fact_checker (reviewer) |
| Newsroom | настроена (стадии 1–9) | editor → `story_brief` и `release_receipt`, producer → `stage_report` |
| Legal | настроена | legal_counsel → `law_report`, platform_reviewer → `platform_report`, legal_lead → `legal_review` |
| Script | настроена | scriptwriter → `script`, script_editor (reviewer) |
| Video | настроена 28.10 | video_producer (Docker) → `video_package` |
| QA | настроена 28.10 | qa_validator → `validation_report` |
