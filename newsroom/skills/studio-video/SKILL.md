---
name: studio-video
description: Ключ генератора картинок сцен для newsroom/tools/video_build.py (команда Video 8bitnews). Сам ничего не делает: инструмент читает video.env.
---

Файл `video.env` рядом — ключ OpenRouter, которым студия рисует и проверяет картинки сцен. Его читает
`newsroom/tools/video_build.py` через `hublib.load_keys`. Не печатай содержимое `video.env` и не вставляй ключ
в артефакты и ответы.
