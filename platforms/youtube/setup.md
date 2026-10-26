---
verified_at: 2026-10-26
purpose: Инструкция владельцу: как настроить каналы 8bitnews на YouTube и доступ hub к YouTube API.
---

# YouTube: настройка каналов и API

Делается один раз на каждый канал (8BitNewsUS, позже 8BitNewsES). Правила площадки для ревью роликов —
`rules.md`, налоги и AdSense — `operator/legal.md#owner-tasks`.
`[не проверено]` — пункт меню, который не удалось подтвердить в справке; если его нет на месте, пропустите
и напишите мне.

## 1. Google-аккаунт владельца

- Включите двухэтапную аутентификацию (2-Step Verification). Без неё канал не примут в YouTube Partner Program
  ([YPP](https://support.google.com/youtube/answer/72851)). Лучше всего ключ доступа (passkey) или приложение
  Google Authenticator, а не SMS.
- Проверьте резервный email и телефон: канал с политическими новостями — частая цель взлома.
- Если с каналом будет работать кто-то ещё, выдавайте роль в YouTube Studio → Настройки → Разрешения, а не
  пароль от аккаунта. Для загрузки и правки роликов без доступа к доходам хватит роли Editor (Limited)
  ([channel permissions](https://support.google.com/youtube/answer/9481328)).

## 2. Настройки каждого канала в YouTube Studio

**Настройки → Канал → Основная информация**
- **Страна проживания — Испания.** Поле про вас, а не про аудиторию: от него зависят доступ к YPP и налоги.
  Аудитория канала от этого не меняется.
- **Ключевые слова:** `news analysis, US politics, explainer` (для ES — по-испански).

**Настройки → Канал → Расширенные настройки**
- **Аудитория:** «No, set this channel as not made for kids. I almost never upload content that's made for kids»
  ([audience](https://support.google.com/youtube/answer/9527654)). Pixel-art — признак детского контента, поэтому
  ставим это явно (`countries/us/legal.md#regulatory-coppa`).

**Настройки → Канал → Доступ к функциям**
- Подтвердите телефон. Это открывает функции среднего уровня, в том числе собственные обложки
  ([features](https://support.google.com/youtube/answer/9890437)).
- Расширенные функции (кликабельные ссылки, заявка на монетизацию) открываются сами со временем или сразу —
  проверкой по документу или видео. Проверка по документу доступна не всем.

**Настроить вид канала**
- **Название и псевдоним:** `8BitNews US` / `@8BitNewsUS` (для ES — `8BitNews ES` / `@8BitNewsES`).
  Пришлите мне, какие получились.
- **Фото профиля и баннер** — пиксельный логотип 8bitnews. Сделаем вместе с шаблоном в E2; до этого можно
  оставить пустыми. Ничего похожего на логотипы и плашки реальных СМИ.
- **Описание** — ревьюер YPP смотрит раздел «О канале» ([YPP policies](https://support.google.com/youtube/answer/1311392)).
  Черновик для US (адрес для исправлений подставим, когда заведёте почту):

  > 8BitNews explains developing news stories in about a minute: what's new, what's known, how opinions split
  > at home and abroad, and what is still unknown. Every video is built from multiple sources, listed in its
  > description. Narration is a synthetic AI voice and visuals are AI-generated pixel art; real people are shown
  > as stylized illustrations. A human editor reviews every video before it is published.
  > Independent: no funding from parties, campaigns or governments. Corrections: {corrections email}

**Настройки → Параметры загрузки по умолчанию**
Ролики будет загружать hub через API и сам ставить нужные поля. Значения по умолчанию нужны на случай ручной
загрузки:
- категория — News & Politics;
- язык названия и описания, язык видео — English (United States) (для ES — Spanish (Spain));
- лицензия — стандартная лицензия YouTube;
- комментарии включены, с задержкой потенциально неприемлемых комментариев на проверку;
- **ремиксы Shorts — запретить** `[не проверено: пункт «Shorts remixing»]`. Это аналог выключенных Duet
  и Stitch в TikTok: новостной ролик легко вырвать из контекста.

**При каждой ручной загрузке** в разделе «Атрибуты» ставьте **«AI use» → Yes**
([disclosure](https://support.google.com/youtube/answer/14328491)). YouTube не требует метку для
нереалистичной анимации, но наша политика ставит её всегда (`countries/us/policy.md#раскрытие`). В API это
поле `status.containsSyntheticMedia`.

## 3. Пока ничего не публикуйте

- Не загружайте тестовые и случайные ролики. Ревьюер YPP смотрит самые новые и самые популярные ролики канала,
  и мусор в начале истории канала вредит заявке.
- Канал ES до окончания выборов 29.11.2026 стоит пустым (`operator/legal.md#owner-tasks`).
- Платное продвижение роликов о политике не включайте: это политическая реклама, в ЕС она запрещена
  (`countries/us/policy.md`).

## 4. Доступ hub к YouTube API (Google Cloud)

Один проект на все каналы. Оплата в Google Cloud для YouTube API не нужна.

1. [console.cloud.google.com](https://console.cloud.google.com) → создайте проект `8bitnews-publisher`.
2. **APIs & Services → Library** → включите **YouTube Data API v3** и **YouTube Analytics API**.
3. **Google Auth Platform** (в старом интерфейсе — OAuth consent screen):
   - **Branding:** название `8bitnews publisher`, email поддержки — ваш. Логотип не ставьте: с логотипом Google
     требует проверку бренда.
   - **Audience:** тип пользователей — External.
   - **Data access → Add scopes:**
     - `https://www.googleapis.com/auth/youtube.upload` — загрузка;
     - `https://www.googleapis.com/auth/youtube.force-ssl` — смена видимости и удаление: это кнопка
       «Снять с публикации»;
     - `https://www.googleapis.com/auth/yt-analytics.readonly` — статистика для DataLens.
   - **Audience → Publish app → In production.** Важно: у приложения в статусе Testing refresh token (токен, по которому hub входит
     без вас) живёт 7 дней, и hub пришлось бы заново авторизовывать каждую неделю
     ([OAuth](https://developers.google.com/identity/protocols/oauth2)).
     Проверку приложения Google не проходим. При входе вы увидите экран «Google hasn't verified this app» →
     Advanced → перейти в приложение. Непроверенному приложению доступны 100 пользователей
     ([unverified apps](https://support.google.com/cloud/answer/7454865)); нам нужен один — вы.
4. **Clients** — OAuth-клиент создадим, когда у hub будет адрес: тип Web application, redirect URI
   `https://<адрес hub>/oauth/youtube/callback`. Каждый канал вы авторизуете отдельно: при входе выбираете нужный
   канал в списке аккаунтов.
5. **Секреты.** Client secret и JSON клиента не присылайте в чат и не кладите в репозиторий. Храните их
   в менеджере паролей; в hub они попадут через переменные окружения.
6. **Квота.** Ничего не запрашивайте: у загрузки отдельный лимит — 100 вызовов в день на проект
   (`rules.md`, раздел 8). Двум каналам по 1–3 ролика в день хватает.

**Открытый вопрос, проверим вместе.** Справка YouTube противоречит сама себе: видео, загруженные через
непроверенный API-проект, то ли публикуются как обычно, то ли запираются в private до аудита (`rules.md`,
раздел 8). Когда будет клиент, загрузим один ролик со статусом unlisted, посмотрим, что поставит YouTube,
и удалим его.

## 5. Монетизация: чего ждать

- **Сейчас** ([YPP](https://support.google.com/youtube/answer/72851)):
  - донаты и магазин — от 500 подписчиков, 3 публичных роликов за 90 дней и 3 млн просмотров Shorts за 90 дней
    ([expanded YPP](https://support.google.com/youtube/answer/13429240));
  - доход от рекламы — от 1000 подписчиков и 10 млн просмотров Shorts за 90 дней.
- **С 01.02.2027** порог для новых каналов вырастает до 1000 подписчиков и 20 млн просмотров Shorts
  за 90 дней. Чтобы получать долю из пула Shorts, нужно держать 10 млн просмотров за 90 дней
  ([changes](https://support.google.com/youtube/answer/12843009)). Новому каналу до февраля 10 млн, скорее
  всего, не набрать, так что рассчитывайте на новые пороги.
- **Когда дойдёт до заявки:** один AdSense for YouTube на вас, к нему привязываются оба канала. В AdSense —
  налоговая форма W-8BEN с испанским NIF (`operator/legal.md#us-tax-withholding`).

## 6. Что прислать мне

- Для каждого канала: псевдоним (`@…`) и ID канала (`UC…`; виден на youtube.com/account_advanced). Внесу их
  в `channels/<id>/channel.yaml`.
- ID проекта Google Cloud — когда проект создан и приложение переведено в In production.
- Что из этой инструкции не нашлось на месте или выглядит иначе.
