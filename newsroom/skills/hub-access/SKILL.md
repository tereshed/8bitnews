---
name: hub-access
description: Ключ hub 8bitnews для инструментов newsroom/tools (video_build, video_review_kit, release_submit). Сам ничего не делает: инструменты читают hub.env.
---

Файл `hub.env` рядом — адрес hub и его ключ. Его читают скрипты `newsroom/tools/*.py` через `hublib.load_keys`.
Не печатай содержимое `hub.env`, не передавай ключ никуда, кроме hub, и не вставляй его в артефакты и ответы.
