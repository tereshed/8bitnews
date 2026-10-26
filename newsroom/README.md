# newsroom — конфигурация контент-команд PolyMaths

Источник правды для промптов агентов и контрактов артефактов контент-команд (см. `docs/architecture.md` §5).
Настройки в PolyMaths синхронизируются отсюда: `python3 newsroom/sync.py <team> [--dry-run]`
(ключ — env `POLYMATHS_API_KEY` или `.mcp.json`). Правки в интерфейсе PolyMaths следующий запуск перезапишет.

- `teams/<team>/team.json` — манифест команды: `team_id`, описание для делегирования, агенты, контракты.
- `teams/<team>/<agent>.md` — промпт агента. Итоговый `system_prompt` = файл агента + `teams/<team>/_common.md`
  (кроме `router.md`, он берётся как есть).
- `tools/pm_artifact.py` — агенты скачивают по id артефакты, которые Newsroom передаёт командам
  (`docs/architecture.md` §5, «Передача данных между командами»).
- `contracts/<kind>.schema.json` — JSON Schema (draft 2020-12) контракта артефакта. Общие части (источник, факт,
  группа мнений) повторяются в нескольких схемах, поэтому JSON генерируется: правь `gen_contracts.py` и запускай
  `python3 newsroom/gen_contracts.py`, затем `sync.py`.

## Команды

| Команда | Статус | Агенты → артефакт |
|---|---|---|
| Research | настроена | fact_researcher → `facts_report`, local_press_analyst → `local_press_report`, world_press_analyst → `world_press_report`, analyst → `research_dossier`, fact_checker (reviewer) |
| Newsroom | настроена (стадии 1–5) | editor → `story_brief`, producer → `stage_report`; `release_receipt` — в E4 |
| Legal | настроена | legal_counsel → `law_report`, platform_reviewer → `platform_report`, legal_lead → `legal_review` |
| Script | настроена | scriptwriter → `script`, script_editor (reviewer) |
| Video | E3 | video_producer → `video_package` |
| QA | E3 | tech_validator, content_validator → `validation_report` |
