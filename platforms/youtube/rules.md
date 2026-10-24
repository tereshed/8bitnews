---
verified_at: 2026-10-24
purpose: База правил YouTube (Shorts) для LLM-агентов, которые проверяют ролики 8bitnews на риск страйков, бана, демонетизации и урезания охвата.
---

# YouTube: правила площадки для ревью роликов

Обозначения: `[не проверено]` — не удалось подтвердить на официальной странице на дату `verified_at`;
`→` — вывод или рекомендация для нашей модели канала, а не цитата правил. Цитаты политик даны на английском.

## 1. Санкции и апелляции

- Первое нарушение Community Guidelines (CG) обычно даёт только warning. Если пройти optional policy training, warning истекает через 90 дней, причём отсчёт идёт от прохождения тренинга. Повтор той же политики в эти 90 дней может дать страйк ([strike basics](https://support.google.com/youtube/answer/2802032)).
- 1-й страйк: 1 неделя без загрузок, Premieres, запланированных публикаций, custom thumbnails, posts и правок плейлистов. Запланированные публичные ролики переводятся в private ([strike basics](https://support.google.com/youtube/answer/2802032)).
- 2-й страйк в те же 90 дней: 2 недели запрета на публикацию. 3 страйка за 90 дней: канал может быть удалён. Каждый страйк живёт 90 дней. Удаление ролика страйк не снимает, а страйк могут выдать и за уже удалённый контент ([strike basics](https://support.google.com/youtube/answer/2802032)).
- Терминация возможна без предупреждения за «a single case of severe abuse (such as predatory behavior, spam, or pornography)» или за «dedication to a policy violation». Обходить её через другой канал запрещено ([terminations](https://support.google.com/youtube/answer/2802168)).
- Апелляции: warning или strike обжалуют в течение 6 месяцев, удаление контента — в течение 1 года. Каждый страйк обжалуют только один раз, а за отклонённую апелляцию санкций нет ([appeals](https://support.google.com/youtube/answer/185111)). Терминацию обжалуют в течение 1 года, не более 2 апелляций ([terminations](https://support.google.com/youtube/answer/2802168)).
- Копирайт-страйки — отдельная система. Страйк истекает через 90 дней, только если пройти Copyright School, иначе остаётся активным. 3 копирайт-страйка за 90 дней ведут к терминации. Снять страйк можно через retraction или counter notification. Если запрос на удаление помечен как «scheduled», есть 7 дней, чтобы удалить ролик и не получить страйк ([copyright strikes](https://support.google.com/youtube/answer/2814000)).
- Удаление по privacy complaint или по решению суда страйка не даёт ([strike basics](https://support.google.com/youtube/answer/2802032)). Likeness claim тоже не даёт страйка ([likeness claims](https://support.google.com/youtube/answer/17133929)).
- Нарушение политик монетизации ведёт к limited или no ads на ролике, приостановке YPP и удержанию выплат до 90 дней ([YPP policies](https://support.google.com/youtube/answer/1311392)).

## 2. Правила сообщества для новостного контента

**Дезинформация и выборы**
- Запрещено ([elections misinfo](https://support.google.com/youtube/answer/10835034)):
  - voter suppression: ложь о времени, месте, способе голосования и о том, кто имеет право голосовать, а также ложные утверждения, которые «could materially discourage voting»;
  - ложь о «technical eligibility» кандидатов и действующих должностных лиц: возраст, гражданство, «vital status»;
  - призывы мешать демократическим процессам.
- Под те же правила попадают угрозы election workers, имперсонация кандидата или партии и ссылки на запрещённый контент. Вербальная отсылка «иди на такой-то сайт» тоже считается ссылкой ([elections misinfo](https://support.google.com/youtube/answer/10835034)).
- Manipulated content — технически изменённый контент, который вводит в заблуждение «usually beyond clips taken out of context». Misattributed content — старые кадры, выданные за текущее событие ([misinformation](https://support.google.com/youtube/answer/10834785)).
- На текущей странице нет пункта о ложных заявлениях о фальсификации прошлых выборов в США. Изменение января 2025 года касается только выборов в Бразилии 2014, 2018 и 2022 годов ([policy updates](https://support.google.com/youtube/answer/10008196), [elections misinfo](https://support.google.com/youtube/answer/10835034)).

**Насилие и графика**
- Запрещён контент «intended to shock or disgust», трупы с тяжёлыми повреждениями и подстрекательство к насилию. Политика распространяется и на thumbnails, titles и descriptions ([violent/graphic](https://support.google.com/youtube/answer/2802008)).
- Для образовательного и новостного исключения контекст должен быть «in the images or audio of the video itself». Заголовка или описания недостаточно. Вместо удаления YouTube может ограничить ролик по возрасту ([violent/graphic](https://support.google.com/youtube/answer/2802008)).
- Даже с EDSA запрещены «footage filmed by the perpetrator» смертельного или крупного насильственного события и насильственное сексуальное насилие ([EDSA](https://support.google.com/youtube/answer/6345162)).
- Анимацию и игры YouTube отличает от постановок с живыми актёрами. Отдельного правила для анимации, изображающей реальные события, на странице нет `[не проверено]` ([violent/graphic](https://support.google.com/youtube/answer/2802008)).

**Насильственные экстремистские и преступные организации (VECO)**
- Запрещены praise, promotion, aid, вербовка и сбор средств, а также символы и логотипы, если они показаны «in order to praise or promote them», и съёмка исполнителя атаки ([VECO](https://support.google.com/youtube/answer/9229472)).
- Для EDSA-исключения нужно «enough information in the video or audio itself». Такой ролик всё равно могут ограничить по возрасту или закрыть warning screen ([VECO](https://support.google.com/youtube/answer/9229472)).

**Харассмент и узнаваемые люди**
- Обсуждение «individuals who have positions of power» допускается, но «not a pass to harass» ([harassment](https://support.google.com/youtube/answer/2802268)). Запрещены:
  - угрозы и доксинг;
  - «reveling in or mocking the death or serious injury of an identifiable individual»;
  - изображение реального человека убитым или тяжело раненым (пример — фото реального человека вместо лица актёра);
  - AI-копия голоса или внешности для имперсонации.
- С 10 февраля 2025 года запрещён контент, который «realistically simulates deceased individuals describing their death» ([policy updates](https://support.google.com/youtube/answer/10008196)).
- Приватность: узнаваемый человек может потребовать удалить «realistic altered or synthetic version» своей внешности или голоса. YouTube учитывает реалистичность, раскрытие, пародию и общественный интерес, а также участие публичной фигуры в «criminal activity, violence, or endorsing a product or political candidate» ([privacy guidelines](https://support.google.com/youtube/answer/7671399)).
- Likeness claim ставится на AI-контент, который совпал с лицом или голосом человека. Заявитель может заблокировать ролик целиком. AI-раскрытие и статус private или unlisted от такого claim не защищают ([likeness claims](https://support.google.com/youtube/answer/17133929)).

**Несовершеннолетние** (младше 18 лет)
- Запрещено раскрывать личные данные несовершеннолетних, а также травить, позорить или обманывать их ([child safety](https://support.google.com/youtube/answer/2801999)).
- Запрещены «family friendly cartoons» на детскую аудиторию со взрослыми темами (насилие, смерть) ([child safety](https://support.google.com/youtube/answer/2801999)).
- Анимированные и мультяшные персонажи — один из факторов «made for kids», но сам по себе он не делает ролик детским ([made for kids](https://support.google.com/youtube/answer/9528076)).
- → Pixel-art стилистика повышает риск ошибочной классификации. Всегда ставим `selfDeclaredMadeForKids=false` и не используем «детских» сигналов в заголовке, тегах и обложке.

**EDSA и общественный интерес**
- Для EDSA YouTube смотрит, какой контекст есть и где он стоит: кто в кадре, что происходит, когда и где. Дисклеймер вроде «Don't try this at home» может не сработать. Контекст в комментариях, тегах и описании канала может не засчитываться. Для самого опасного контента контекст нужен «in the VIDEO or AUDIO» ([EDSA](https://support.google.com/youtube/answer/6345162)).
- Исключения общественного интереса — «congressional or parliamentary proceedings, campaign speeches, or debates over ongoing government actions». Это «not a pass to promote misinformation» ([elections misinfo](https://support.google.com/youtube/answer/10835034)).

## 3. Синтетический и AI-контент

- Раскрытие обязательно для реалистичного GenAI-контента, который делает одно из следующего ([GenAI disclosure](https://support.google.com/youtube/answer/14328491)):
  - «Makes a real person appear to say or do something they didn't do»;
  - изменяет кадры реального события или места;
  - создаёт реалистичную сцену, которой не было;
  - создаёт музыку, которая является «main focus of the video».
- Не нужно раскрывать «non-realistic content», например «AI-generated or altered animation of a missile in a fully animated video». Также не нужно раскрывать помощь в производстве — сценарий, thumbnail, title, инфографику — и субтитры ([GenAI disclosure](https://support.google.com/youtube/answer/14328491)).
- Среди примеров, требующих раскрытия: «AI generated music» и «Making it look like a real person has been arrested or imprisoned» в реалистичном исполнении ([GenAI disclosure](https://support.google.com/youtube/answer/14328491)).
- Обезличенный TTS-голос на странице прямо не упомянут. Явно указано только, что клонировать собственный голос раскрывать не нужно `[не проверено: нет явного правила]`.
- YouTube может поставить метку сам: на контент из своих AI-инструментов, на контент с C2PA-метаданными и на то, что определят внутренние системы. Метку, поставленную по C2PA или после ручной проверки, автор снять не может ([GenAI disclosure](https://support.google.com/youtube/answer/14328491)).
- Санкции за систематическое нераскрытие: метка вручную, удаление контента, приостановка YPP. По заявлению YouTube, само раскрытие «won't limit a video's audience or impact its eligibility to earn money» ([GenAI disclosure](https://support.google.com/youtube/answer/14328491)).
- **API:** поле `status.containsSyntheticMedia` (boolean): «allows the channel owner to disclose that a video contains realistic Altered or Synthetic (A/S) content». Задаётся в `videos.insert` и `videos.update` ([videos resource](https://developers.google.com/youtube/v3/docs/videos)); появилось 30.10.2024 ([revision history](https://developers.google.com/youtube/v3/revision_history)).
- → Правила YouTube не требуют метки для pixel-art с обезличенным TTS, но по `countries/<cc>/policy.md` канал помечает AI-контент на всех площадках: ставим `containsSyntheticMedia=true` всегда. Раскрытие, по заявлению YouTube, не снижает охват и не влияет на монетизацию. Фотореалистичную генерацию и клоны голосов реальных людей не используем вовсе.

## 4. Монетизация и право на рекомендации

- В YPP контент должен быть «original» и «authentic», «not mass-produced, generic, repetitive». Ревьюер смотрит основную тему канала, самые просматриваемые и самые новые ролики, метаданные и раздел «About» ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- 15.07.2025 политику «repetitious content» переименовали в «inauthentic content» и уточнили, что она покрывает «repetitive or mass-produced» контент. На странице по состоянию на 2026-10-24 раздел называется «Generic or Repetitive Content» ([YPP policies](https://support.google.com/youtube/answer/1311392)).
  - Не монетизируется: «Image slideshows, templated storylines, or scrolling text with minimal or no narrative, commentary, or educational value» и «AI-generated content made with generic or unoriginal templates giving the impression of mass production without adding the creator's original, authentic insights or perspective».
  - Монетизируется: одинаковые intro и outro, если основная часть роликов разная.
- Reused content не монетизируется, если ролик — это «Content that exclusively features readings of other materials you did not originally create, like text from websites or news feeds». Политика применяется к каналу целиком ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- Unsatisfying content: разрешено «using AI to edit your video scripts or generate a unique background visual». Запрещено, в частности, «realistic visuals tricking viewers into believing a fake celebrity death or natural disaster» ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- AI Personas: каналы, где AI-персона «presents itself as a human expert providing advice» о здоровье, праве, финансах или политике, не монетизируются ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- Spam policy — это Community Guidelines, а не только монетизация. Раздел «Automated or synthetic mass-production» запрещает «Using automated tools or AI to churn out high volumes of similar content with minimal changes». Пример из правил: «Channels that use the exact same background music and repetitive AI generated imagery across many videos, with each video reading out an AI-generated script». Санкции: отключение монетизации, warning, страйк, терминация ([spam](https://support.google.com/youtube/answer/2801973)).
- Монетизация Shorts ([Shorts monetization](https://support.google.com/youtube/answer/12504220)):
  - нужен Shorts Monetization Module; просмотры неоригинальных Shorts в выплаты не входят;
  - Shorts длиннее 1 минуты с claimed content блокируются и не монетизируются;
  - если в Short есть музыка, доход делится с музыкальными партнёрами;
  - с 01.02.2027 для доли в доходе от рекламы в Shorts нужно ≥10 млн qualified Shorts views за 90 дней.
- Advertiser-friendly guidelines применяются к видео, thumbnail, title, description и tags ([ad-friendly](https://support.google.com/youtube/answer/6162278)):
  - новостное освещение насилия «in a journalistic context» может монетизироваться;
  - без рекламы: «Footage of war with graphic depictions of injury, death, or suffering in any context», «Video gameplay showing graphic violence directed at real named persons», не-образовательный контент о FTO с «names of the group/leader» в thumbnail;
  - sensitive events: новостной контент может монетизироваться, но «Using keywords related to a sensitive event to attempt to drive additional traffic» оставляет ролик без рекламы.
- Изменения 2026 года ([policy updates](https://support.google.com/youtube/answer/10008196)): в октябре разрешили монетизировать контент общественного интереса (заседания конгресса, дебаты о действиях власти); в августе уточнили, как монетизируются изображения смерти в новостном и документальном контексте.
- С 10.03.2025 решение о монетизации может занимать до 24 часов, включая ручную проверку, в том числе для private-видео ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- → Как снизить риск для автоматически собранных роликов:
  - каждый ролик — собственный анализ с собственной структурой, а не пересказ ленты агентств;
  - менять музыку, композицию кадров и шаблоны от ролика к ролику;
  - не выдавать TTS-ведущего за человека-эксперта;
  - не публиковать почти одинаковые ролики по одной теме подряд.

## 5. Политический контент

- Официальной политики YouTube, которая отдельно ограничивает рекомендации политического контента, на support.google.com/youtube не найдено `[не проверено]`.
- Paid promotion. В сентябре 2026 года «Paid Product Placements» переименовали в «Branded Content Policy». Автоматические системы могут сами поставить метку о спонсорстве ([policy updates](https://support.google.com/youtube/answer/10008196)). В API за это отвечают `paidProductPlacementDetails.hasPaidProductPlacement` ([videos resource](https://developers.google.com/youtube/v3/docs/videos)) и part `brandPartner` (с 07.07.2026, [revision history](https://developers.google.com/youtube/v3/revision_history)).
- ЕС, актуально для Испании. С 10.10.2025 Google не разрешает политическую рекламу в ЕС. Об «EU political paid promotion» автор обязан заявить, после чего ролик становится недоступен в ЕС. Личных неоплаченных мнений правило не касается ([branded content](https://support.google.com/youtube/answer/154235)).
- Информационная панель издателя показывается на каналах «funded by a government, or publicly funded». Она основана на Wikipedia и других источниках и не показывается в поиске ([publisher context](https://support.google.com/youtube/answer/7630512)).
- → Мы не берём оплату за политический контент. Если она появится, это paid promotion с отдельными правилами для ЕС.

## 6. Копирайт и музыка

- Content ID создаёт claim автоматически, когда загруженное видео совпадает с эталоном. Правообладатель может заблокировать, монетизировать или отслеживать ролик, в том числе по странам ([claims](https://support.google.com/youtube/answer/6013276)).
- С 15.10.2024 вертикальные ролики длиной 1–3 минуты считаются Shorts. Shorts длиннее минуты с активным claim блокируются «regardless of the policy», страйка при этом нет ([claims](https://support.google.com/youtube/answer/6013276)).
- → Ролики 61–90 с с любым claim на вшитую музыку будут заблокированы. Способ загрузки (API или Studio) этого не меняет: Content ID сверяет сам загруженный файл.
- Reused content — не копирайт: claim может не прийти, а монетизацию всё равно снимут ([YPP policies](https://support.google.com/youtube/answer/1311392)).
- Ускорение звука, фильтры и обрезка ради обхода детекции — это «Detection evasion» по Spam policy ([spam](https://support.google.com/youtube/answer/2801973)).
- Лицензионные библиотеки, которые регистрируют треки в Content ID, требуют whitelisting канала `[не проверено: зависит от библиотеки, не правило YouTube]`.

## 7. Метаданные: заголовки, обложки, хэштеги

- «Malicious clickbait» — «maliciously misleading titles, thumbnails, descriptions, or imagery», когда ролик не даёт обещанного ([spam](https://support.google.com/youtube/answer/2801973)).
- С января 2025 года YouTube усиливает борьбу с вводящими в заблуждение метаданными, «especially when that content focuses on breaking news, current events, or sensitive topics like elections or politics». Начали с Индии, дальше обещаны другие страны ([policy updates](https://support.google.com/youtube/answer/10008196)). Распространилось ли это на США — `[не проверено]`.
- Thumbnails: запрещены «Violent imagery that intends to shock or disgust», «Graphic or disturbing imagery with blood or gore» и обложки, которые вводят в заблуждение о содержании ролика. В пограничном случае обложку снимут или ограничат по возрасту без страйка; при нарушении возможен страйк ([thumbnails](https://support.google.com/youtube/answer/9229980)).
- Hashtags: если их больше 60, YouTube игнорирует все. Нерелевантные и вводящие в заблуждение хэштеги ведут к удалению ролика, как и харассящие или разжигающие ненависть. Повторяющиеся описательные теги в описании запрещены ([hashtags](https://support.google.com/youtube/answer/6390658)).
- Мат в title или thumbnail ограничивает рекламу ([ad-friendly](https://support.google.com/youtube/answer/6162278)).
- Максимальная длина title — 100 символов ([developer policies](https://developers.google.com/youtube/terms/developer-policies)).

## 8. Ограничения API, влияющие на соответствие правилам

- `videos.insert`: с 01.06.2026 у метода свой quota bucket; «Quota impact: 100 calls per day», 1 единица за вызов. Увеличение квоты — через Quota Extension Form ([videos.insert](https://developers.google.com/youtube/v3/docs/videos/insert), [revision history](https://developers.google.com/youtube/v3/revision_history)).
- Если `status.privacyStatus` не задан, по умолчанию ставится `public`. Текущая справка: «Videos uploaded from unverified API projects are not restricted to private viewing mode» ([videos.insert](https://developers.google.com/youtube/v3/docs/videos/insert)). Запись от 28.07.2020 говорила обратное ([revision history](https://developers.google.com/youtube/v3/revision_history)). → Проверить на тестовом канале.
- Поля, которые можно задать при загрузке ([videos.insert](https://developers.google.com/youtube/v3/docs/videos/insert)):
  - `snippet.title`, `snippet.description`, `snippet.tags[]`, `snippet.categoryId`, `snippet.defaultLanguage`;
  - `status.privacyStatus`, `status.publishAt` (только вместе с `private`), `status.selfDeclaredMadeForKids`, `status.containsSyntheticMedia`, `status.license`, `status.embeddable`;
  - `recordingDetails.recordingDate`;
  - части (part) `paidProductPlacementDetails` и `brandPartner`.
- Developer Policies ([developer policies](https://developers.google.com/youtube/terms/developer-policies)):
  - «users must have final control over the data that will be published»;
  - нельзя «automate or trigger … uploads … without the user's prior specific and express consent»;
  - клиент с загрузкой обязан «clearly display an option» выбрать privacy status;
  - нельзя дописывать текст в описание без согласия пользователя.
  → Одобрение человеком в hub закрывает требование о согласии. Выбор privacy status надо показывать в интерфейсе модерации.
- С 15.10.2026 `selfDeclaredMadeForKids` и `madeForKids` обновляются, только если явно указаны в запросе ([revision history](https://developers.google.com/youtube/v3/revision_history)).

## 9. Чек-лист «красные флаги» для ревьюера

| Паттерн в ролике | Последствие | Как исправить |
|---|---|---|
| Та же музыка, тот же визуальный шаблон и AI-скрипт, что в прошлых роликах канала ([spam](https://support.google.com/youtube/answer/2801973)) | Warning или страйк, отключение монетизации, терминация | Менять трек, композицию и шаблон; в каждом ролике — своя аналитическая структура |
| Скрипт — пересказ одной статьи или ленты агентства без анализа ([YPP](https://support.google.com/youtube/answer/1311392)) | Reused content, демонетизация канала | Синтез нескольких источников, разбор мнений, блок «неизвестно пока» |
| Слайд-шоу статичных картинок с минимальным нарративом ([YPP](https://support.google.com/youtube/answer/1311392)) | Inauthentic content, демонетизация | Анимация, смена сцен по смыслу, авторский нарратив |
| TTS-ведущий представлен как живой эксперт и даёт политические советы ([YPP](https://support.google.com/youtube/answer/1311392)) | Нет монетизации канала (AI Personas) | Не выдавать голос за человека, не давать советов «как голосовать» |
| Неверные дата, место или способ голосования, ложь о праве кандидата баллотироваться ([elections](https://support.google.com/youtube/answer/10835034)) | Удаление, warning или страйк | Сверять с сайтами избиркомов штатов (в Испании — Junta Electoral Central); при сомнении убрать |
| Реалистичный «фото»-кадр с политиком в наручниках или в сцене, которой не было ([GenAI](https://support.google.com/youtube/answer/14328491), [privacy](https://support.google.com/youtube/answer/7671399)) | Обязательное раскрытие; при нераскрытии метка, удаление, санкции YPP; privacy-жалоба | Только pixel-art; если реалистично — `containsSyntheticMedia=true`, а лучше не делать |
| Pixel-art сцена смерти или ранения реального узнаваемого человека ([harassment](https://support.google.com/youtube/answer/2802268), [ad-friendly](https://support.google.com/youtube/answer/6162278)) | Удаление по harassment, no ads | Показывать последствия или символы, не сам момент насилия над человеком |
| Графика с кровью или телами, контекст только в описании ([violent](https://support.google.com/youtube/answer/2802008)) | Удаление или age-restriction | Контекст голосом и текстом в кадре: кто, что, где, когда, зачем |
| Логотип или флаг террористической группы крупно, без осуждения ([VECO](https://support.google.com/youtube/answer/9229472), [ad-friendly](https://support.google.com/youtube/answer/6162278)) | Удаление; no ads, если символ в thumbnail | Символы мелко или размыто, в кадре явная новостная рамка |
| Кадры или цитаты, снятые исполнителем атаки ([EDSA](https://support.google.com/youtube/answer/6345162)) | Удаление даже с EDSA | Не использовать, даже стилизованно |
| Лицо, имя или школа несовершеннолетнего (подозреваемого или жертвы) ([child safety](https://support.google.com/youtube/answer/2801999)) | Удаление, страйк | Не называть и не изображать узнаваемо |
| Детские маркеры в стиле, заголовке или тегах ([MFK](https://support.google.com/youtube/answer/9528076)) | Ошибочный made for kids, проблемы child safety | `selfDeclaredMadeForKids=false`, взрослые формулировки |
| Заголовок или обложка обещают то, чего нет в ролике («СЛИВ», «ШОК») ([spam](https://support.google.com/youtube/answer/2801973)) | Удаление, warning, страйк | Заголовок = факт из ролика |
| Обложка с кровью или телами ([thumbnails](https://support.google.com/youtube/answer/9229980)) | Обложку снимут, возможен страйк, limited ads | Нейтральная обложка: портрет или локация |
| Больше 60 хэштегов или нерелевантные трендовые теги ([hashtags](https://support.google.com/youtube/answer/6390658)) | Хэштеги игнорируются, ролик могут удалить | 3–5 релевантных хэштегов |
| Ролик 61–90 с, музыка из библиотеки, зарегистрированной в Content ID ([claims](https://support.google.com/youtube/answer/6013276)) | Блокировка Short, без монетизации | Royalty-free без Content ID, whitelisting или ролик ≤60 с |
| Ускоренная или «замаскированная» чужая музыка ([spam](https://support.google.com/youtube/answer/2801973)) | Detection evasion → Spam policy | Только лицензированный трек без модификаций ради обхода |
| Ролик за деньги в пользу партии или кандидата ([branded content](https://support.google.com/youtube/answer/154235)) | Нужно раскрытие; в ЕС ролик недоступен | Не брать политических заказов; иначе `paidProductPlacementDetails` |
