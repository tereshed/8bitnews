# newsroom — конфигурация контент-команд PolyMaths

Источник правды для промптов агентов и контрактов артефактов контент-команд (см. `docs/architecture.md` §5).
Настройки в PolyMaths синхронизируются отсюда: пока вручную, позже — скриптом.

- `teams/<team>/<agent>.md` — промпт агента. Итоговый `system_prompt` = файл агента + `teams/<team>/_common.md`
  (кроме `router.md`, он берётся как есть).
- `contracts/<kind>.schema.json` — JSON Schema (draft 2020-12) контракта артефакта. Определения общих частей
  (источник, факт, группа мнений) повторяются в каждой схеме и должны совпадать.

## Команды

| Команда | Статус | Агенты → артефакт |
|---|---|---|
| Research | настроена | fact_researcher → `facts_report`, local_press_analyst → `local_press_report`, world_press_analyst → `world_press_report`, analyst → `research_dossier`, fact_checker (reviewer) |
| Newsroom | E3 | editor → `story_brief`, `release_receipt` |
| Script | E3 | scriptwriter → `script`, script_editor (reviewer) |
| Video | E3 | video_producer → `video_package` |
| QA | E3 | tech_validator, content_validator, policy_validator → `validation_report` |
