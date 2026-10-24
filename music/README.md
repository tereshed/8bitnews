# music — каталог фоновой музыки

Источник правды о треках, которые можно подкладывать под ролики (`docs/architecture.md` §5, «Музыка»).
Аудио и доказательства лицензий лежат в S3, здесь — метаданные. hub синхронизирует каталог в таблицу `music_tracks`
и выдаёт треки через `music_pick` только со статусом `approved`.

## Откуда треки

**[решено]** Пул треков от AI-сервиса с коммерческой лицензией, сгенерированный заранее; каждый трек прослушивает
человек. Генерации под каждый ролик нет.

1. `briefs.yaml` — задания на генерацию: настроение, энергия, темп, стиль и промпт.
2. Генерация — по 2 варианта на задание с запасом на отбор. Каждый результат попадает в каталог со статусом
   `candidate` вместе с квитанцией генерации.
3. Прослушивание — человек ставит `approved` или `rejected` и отмечает, не напоминает ли трек известную мелодию.
4. Инцидент на площадке (claim, заглушённый звук) — трек получает статус `disabled`, причина пишется в `review.notes`.

## Формат `catalog.yaml`

```yaml
tracks:
  - id: chip-tense-01            # <стиль>-<настроение>-<номер>
    status: approved             # candidate | approved | rejected | disabled
    origin:
      kind: generated            # generated | purchased | commissioned
      provider: tbd              # сервис генерации
      plan: tbd                  # тариф на момент генерации: от него зависит коммерческая лицензия
      terms_url: https://…       # условия сервиса
      terms_version: 2026-10-01  # редакция условий на момент генерации
      generation_id: …           # ID генерации у сервиса
      prompt: "…"
      generated_at: 2026-10-25T10:00:00Z
      receipt: licenses/chip-tense-01.json   # ключ в S3: ответ API, ID генерации, подтверждение тарифа
    license:
      commercial: true
      platforms: [youtube, tiktok, instagram]
      attribution: null          # текст, если сервис требует атрибуцию в описании
      content_id: none           # none | registered_whitelisted | registered
    audio:
      key: music/tracks/chip-tense-01.wav    # ключ в S3
      duration_s: 92
      bpm: 120
      loopable: true
    tags:
      mood: [tense]              # см. ниже
      energy: 3                  # 1 (фон) … 5 (драйв)
    review:
      listened_at: 2026-10-25
      notes: ""
```

Настроения: `neutral` — нейтральный фон под объяснение, `tense` — напряжение, конфликт, `urgent` — срочная новость,
`somber` — мрачный эмбиент для трагедий (тихо, без ритма), `hopeful` — разрядка, хорошие новости,
`curious` — расследование, загадка.

Схему каталога и её проверку в `cmd/profiles validate` делает эпик E2.
