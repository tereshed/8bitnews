---
verified_at: 2026-10-24
purpose: База правил TikTok для LLM-агентов, которые проверяют ролики 8bitnews на риск удаления, бана, потери For You feed и монетизации.
---

# TikTok: правила площадки для ревью роликов

Обозначения: `[не проверено]` — не удалось подтвердить на официальной странице на дату `verified_at`;
`→` — вывод или рекомендация для нашей модели канала. Действующая редакция Community Guidelines (CG) опубликована 25.08.2026 и вступила в силу 24.09.2026 ([CG overview](https://www.tiktok.com/community-guidelines/en/overview)).
В CG у каждого правила есть уровни: NOT ALLOWED (удаление), AGE-RESTRICTED (только 18+), FYF INELIGIBLE (не попадает в рекомендации), ALLOWED, а у AIGC ещё REQUIRED DISCLOSURE ([CG overview](https://www.tiktok.com/community-guidelines/en/overview), [integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).

## 1. Санкции и апелляции

- Возможные меры: warning, временное ограничение функций или аккаунта, полный бан. После некоторых нарушений можно пройти optional policy training и вернуть доступ ([accounts](https://www.tiktok.com/community-guidelines/en/accounts-features)).
- Account status отражает историю нарушений за последние 90 дней: Healthy, Fair, Severe или Banned. Нарушения истекают через 90 дней ([violations & bans](https://support.tiktok.com/en/safety-hc/account-and-user-safety/content-violations-and-bans)).
- Вес нарушения зависит от тяжести политики и от функции, где оно произошло. Повторы снижают статус, ведут к ограничениям или бану. Удаление ролика нарушение не снимает ([violations & bans](https://support.tiktok.com/en/safety-hc/account-and-user-safety/content-violations-and-bans)).
- Постоянный бан ([violations & bans](https://support.tiktok.com/en/safety-hc/account-and-user-safety/content-violations-and-bans), [accounts](https://www.tiktok.com/community-guidelines/en/accounts-features)):
  - за одно тяжёлое нарушение: CSAM, пропаганда насилия или угрозы им, «content that depicts real-world torture»;
  - за обманную имперсонацию;
  - за обход ограничений через другой аккаунт;
  - за многократные нарушения.
  Могут забанить все связанные аккаунты.
- News и Government/Politician/Political Party аккаунты подчиняются тем же правилам контента, но санкции к ним другие. При достижении общего лимита нарушений аккаунт на 90 дней пропадает из For You и Following. В периоды высокого риска (выборы, беспорядки) за насилие, ненависть или дезинформацию возможен запрет на публикацию на 7–30 дней ([violations & bans](https://support.tiktok.com/en/safety-hc/account-and-user-safety/content-violations-and-bans)).
- Если аккаунт регулярно публикует контент, не подходящий для FYF, весь аккаунт может стать «ineligible for recommendation» и хуже находиться в поиске. Это обжалуется через Inbox или профиль ([account not recommended](https://www.tiktok.com/support/faq_detail?id=7543604783110969862&lang=en)).
- Удаление ролика, бан и FYF-ineligible решения обжалуются в приложении. Причину FYF-ineligible видно в Analytics ([enforcement](https://www.tiktok.com/community-guidelines/en/enforcement), [violations & bans](https://support.tiktok.com/en/safety-hc/account-and-user-safety/content-violations-and-bans)).
- Сроков подачи общих апелляций на страницах нет `[не проверено]`. Для Creator Rewards Program: 80 дней на обжалование неподходящего ролика, 30 дней — дисквалификации аккаунта ([CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)).
- Повторное нарушение прав на интеллектуальную собственность ведёт к бану по repeat infringer policy ([IP policy](https://www.tiktok.com/legal/page/global/copyright-policy/en)).

## 2. Правила сообщества для новостного контента

**Исключения общественного интереса**: Documentary, Educational, Medical/Scientific, Counterspeech, Satirical, Artistic. Даже при исключении TikTok может:
- сделать ролик FYF-ineligible;
- добавить warning screen или context label.
Чтобы контекст был понятен, советуют использовать «captions, stickers, or voiceover». Исключений для «extreme harm» нет ([enforcement](https://www.tiktok.com/community-guidelines/en/enforcement)).

**Дезинформация, выборы, гражданские процессы** ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity))
- NOT ALLOWED:
  - дезинформация, грозящая «significant harm»;
  - прошлые кризисы, выданные за текущие;
  - ложь о том, как, когда и где голосовать, о праве голоса и квалификации кандидатов, о процедурах и результатах выборов;
  - призывы к незаконному срыву итогов выборов;
  - платная политическая реклама.
- FYF INELIGIBLE:
  - «Unverified claims about a crisis or major civic event»;
  - «Unverified claims about an election, such as claiming all ballots are counted before a final tally»;
  - искажение официальной гражданской информации;
  - немонтированное видео, выданное за другое событие;
  - контент на проверке у фактчекеров;
  - конспирология, которая обвиняет «the government» и подрывает доверие.
- ALLOWED: личное мнение без вредной дезинформации; документальный и образовательный контент, который разоблачает дезинформацию.
- → Для breaking news с неподтверждёнными данными прямо помечаем в кадре, что известно и что не подтверждено. Иначе ролик может выпасть из FYF.

**Насилие и графика** ([sensitive themes](https://www.tiktok.com/community-guidelines/en/sensitive-mature-themes))
- NOT ALLOWED: изображения реальных «graphic incidents and violence, such as torture, the moment of someone's death, or the shooting of an individual», мёртвых тел и тяжёлых травм.
- FYF INELIGIBLE и AGE-RESTRICTED: обильная кровь, драки, графика в контексте общественного интереса (война, катастрофы).
- FYF INELIGIBLE: «Fictional graphic incidents», момент перед травмой, «jump scare» эффекты.
- → Pixel-art реконструкция момента выстрела в реального человека близка к «depictions of real-world… shooting of an individual». Безопаснее показывать место, последствия и символы.
- Угрозы, прославление насилия и инструкции по совершению преступлений — NOT ALLOWED. Документальный контент и критика — ALLOWED ([safety & civility](https://www.tiktok.com/community-guidelines/en/safety-civility)).

**Ненависть и харассмент узнаваемых людей** ([safety & civility](https://www.tiktok.com/community-guidelines/en/safety-civility))
- Hate: NOT ALLOWED — дегуманизация, «Portraying protected groups as inherently dangerous, such as calling them criminals», обвинение всей группы за действия одного человека. FYF INELIGIBLE — стереотипы и обобщения. ALLOWED — обсуждение политики, затрагивающей защищённые группы, без атак.
- Харассмент: к публичным фигурам допускается «more critical commentary», но NOT ALLOWED:
  - оскорбления внешности;
  - «Degrading or revictimizing people who have experienced a tragedy»;
  - пожелания смерти или вреда, доксинг, призывы к травле.
- Публичные фигуры — люди старше 18 лет со значимой публичной ролью. Все, кто младше 18, всегда считаются частными лицами.

**Опасные организации и экстремизм** ([safety & civility](https://www.tiktok.com/community-guidelines/en/safety-civility))
- NOT ALLOWED: продвижение и поддержка, а также «Showing people, symbols, objects, speeches, or manifestos linked to» hateful organizations, авторами массового насилия, violent criminal organizations и violent extremist entities.
- ALLOWED: документальный и образовательный контент, «Criticism and satire», обсуждение violent political organizations без продвижения насилия. Нейтральная цитата организации «must make clear that there is no intent to promote it».

**Несовершеннолетние**
- NOT ALLOWED: «Sharing content that could identify a young person who has experienced abuse or exploitation» ([safety & civility](https://www.tiktok.com/community-guidelines/en/safety-civility)).
- AI-контент с «the likeness of young people under the age of 18» запрещён полностью ([AIGC help](https://support.tiktok.com/en/using-tiktok/creating-videos/ai-generated-content)).
- Внешность и голос человека («Image/audio likeness») считаются moderate-risk personal information ([privacy & security](https://www.tiktok.com/community-guidelines/en/privacy-security)).

## 3. Синтетический и AI-контент

- AIGC — любое изображение, видео или аудио, созданное или изменённое AI, «including… artistic styles, like anime, cartoons» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- REQUIRED DISCLOSURE: контент, который «uses AI or includes significant edits to show realistic-looking people or scenes». Примеры: замена лица; AI заставляет человека сказать то, чего он не говорил; «AI-generated audio mimics the voice of a real person» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Раскрытие не нужно ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)):
  - «Using artistic styles, like anime»;
  - «Using generic text-to-speech (TTS) narration, when the TTS isn't a recognizable voice of a known individual».
- NOT ALLOWED даже с меткой ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)):
  - внешность частных лиц без согласия;
  - AI-образы для травли;
  - AIGC, который вводит в заблуждение о важном для общества: «Content made to look like it comes from a real news source», кризисное событие, «A public figure being degraded, harassed, or linked to criminal behavior», политические позиции, которых человек не высказывал, «A political endorsement or condemnation that never happened».
- FYF INELIGIBLE: реалистичный контент, ещё не подтверждённый как AIGC, который может исказить важную тему. ALLOWED: «Humor or art, such as a spoof, meme» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Справка: AIGC запрещён, если «falsely shows public figures in certain contexts» ([AIGC help](https://support.tiktok.com/en/using-tiktok/creating-videos/ai-generated-content)).
- Метка «creator labeled as AI-generated» не влияет на распространение, пока нет нарушений. Auto label «AI-generated» TikTok ставит сам, в том числе по Content Credentials (C2PA), и снять его нельзя ([AIGC help](https://support.tiktok.com/en/using-tiktok/creating-videos/ai-generated-content)).
- Likeness Detection: верифицированные авторы старше 18 лет могут включить сканирование своего лица в контенте, помеченном или определённом как AI-generated, и жаловаться на совпадения ([likeness detection](https://www.tiktok.com/support/faq_detail?id=7670780803776944661&lang=en)).
- **API:** поле `post_info.is_aigc` (bool): «Set to true if the video is AI generated content. If set, the video will be labelled with Creator labeled as AI-generated tag» ([Direct Post API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)).
- → Pixel-art с обезличенным TTS по CG раскрытия не требует, но по `countries/<cc>/policy.md` канал помечает AI-контент на всех площадках: ставим `is_aigc=true` всегда. Метка «creator labeled as AI-generated», по справке TikTok, не влияет на распространение. Узнаваемые политики в pixel-art допустимы, только если сцена соответствует фактам: «linked to criminal behavior» запрещено, если это неправда.

## 4. Монетизация и право на For You feed

- FYF не гарантирован. Ролики уровня FYF INELIGIBLE остаются доступны через поиск и профиль. Во время кризисов и беспорядков TikTok может прерывать однотипные рекомендации ([FYF standards](https://www.tiktok.com/community-guidelines/en/fyf-standards)).
- Оригинальность: «Content is also ineligible for the FYF if it includes unoriginal or reused material without anything new». FYF INELIGIBLE: «Reused or unoriginal content posted without creative edits» (например, чужой watermark) и «Low-quality or minimally edited content» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Spam — NOT ALLOWED: «Using automation to run many accounts or send repetitive content», «Posting a large amount of irrelevant material». Также «We strictly prohibit automation tools, scripts, or other tricks designed to bypass our systems» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Creator Rewards Program (CRP) ([CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)):
  - доступен в США, Великобритании, Германии, Японии, Южной Корее, Франции, Мексике и Бразилии; **Испании в списке нет**;
  - только Personal accounts; Business и политические аккаунты не участвуют;
  - ≥10 000 подписчиков и ≥100 000 просмотров за 30 дней;
  - ролик — «original, high-quality content over 1 minute long», ≥1000 просмотров в FYF, 1080p+;
  - реклама и спонсорский контент вознаграждения не получают.
- CRP не засчитывает контент, неподходящий для FYF, включая ([CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)):
  - «Unoriginal… minimal original input or edits»;
  - «Low quality: … low-quality images, or slide videos»;
  - «Clickbait»;
  - «Disturbing: … graphics or scenes that could upset or shock viewers».
  Пороги: 5 нарушений по роликам за 30 дней — потеря доступа; 5 нарушений аккаунта — исключение навсегда.
- → Ролики 45–60 с в CRP не попадают в принципе. Чтобы снизить риски для автоматически собранных роликов:
  - реальная анимация вместо слайд-шоу;
  - разные визуальные решения;
  - собственный анализ;
  - мягкая подача тяжёлых тем.

## 5. Политический контент

- Платная политическая реклама запрещена, в том числе «creators being compensated for making branded political content» и Promote. Подозрение на платную политическую рекламу делает ролик FYF-ineligible ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Branded Content Policy: «We do not allow paid political advertising». Также запрещён брендированный контент про sensitive events, где есть «political campaigning» ([branded content](https://www.tiktok.com/legal/page/global/bc-policy/en)).
- GPPPA — государственные органы, кандидаты и избранные должностные лица, партии, PAC. Им запрещены монетизация, CRP, реклама и сбор пожертвований. В США такие аккаунты обязаны пройти верификацию ([GPPPA](https://support.tiktok.com/en/using-tiktok/growing-your-audience/government-politician-and-political-party-accounts)).
- → Наш канал — не GPPPA. Публиковать ролики о выборах можно, но нельзя брать оплату от политических акторов.
- TikTok может добавлять warning labels к контенту, который фактчекеры не смогли подтвердить ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- CIO: запрещено публиковать «content on behalf of foreign entities (like a government or military) without saying so» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)).
- Метки государственных СМИ на актуальной справочной странице TikTok подтвердить не удалось `[не проверено]`.

## 6. Копирайт и музыка

- Нарушение чужих прав на интеллектуальную собственность — NOT ALLOWED, контент удаляют ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)). Повторные нарушения ведут к бану, тяжёлые — к немедленному бану ([IP policy](https://www.tiktok.com/legal/page/global/copyright-policy/en)).
- Music Usage Confirmation: автор подтверждает, что в видео нет защищённой музыки или что он «obtained and paid for all necessary licenses (including… musical compositions and the master recordings)». Иначе «the post might be taken down» ([MUC](https://www.tiktok.com/legal/page/global/music-usage-confirmation/en)).
- API-клиент обязан показать эту декларацию перед кнопкой публикации ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines)).
- → Музыка, вшитая в файл, — наша ответственность. Нужна лицензия на композицию и на запись с правом коммерческого использования на TikTok.
- Что бизнес-аккаунтам доступна только Commercial Music Library и что звук нелицензированных треков заглушают, на официальных страницах подтвердить не удалось `[не проверено]`. Для политических аккаунтов страница GPPPA упоминает библиотеку «Commercial Music» ([GPPPA](https://support.tiktok.com/en/using-tiktok/growing-your-audience/government-politician-and-political-party-accounts)).

## 7. Метаданные: подписи, обложки, хэштеги

- FYF INELIGIBLE: «Misleading claims meant to boost views or popularity» и «Like-for-like» ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)). Clickbait делает контент неподходящим для CRP ([CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)).
- В поисковых подсказках TikTok не продвигает термины, связанные с «harassment, sensationalism, or graphic and disturbing themes» ([accounts & features](https://www.tiktok.com/community-guidelines/en/accounts-features)).
- Подпись — поле `title`: до 2200 UTF-16 runes, хэштеги и упоминания распознаются автоматически. Обложка — `video_cover_timestamp_ms`, по умолчанию первый кадр ([Direct Post API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)).
- → Первый кадр становится обложкой: на нём не должно быть крови, тел и символики экстремистов.
- Отдельного лимита на число хэштегов в CG не найдено `[не проверено]`.
- Недискложенный коммерческий контент может получить метку от TikTok или потерять FYF. Повторы ведут к ограничениям и бану ([accounts](https://www.tiktok.com/community-guidelines/en/accounts-features)).

## 8. Ограничения Content Posting API

- **Без аудита публикация только приватная.** «All content posted by unaudited clients will be restricted to private viewing mode» ([Direct Post API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)). Ограничения неаудированного клиента ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines)):
  - до 5 пользователей за 24 часа;
  - аккаунты на момент публикации должны быть private;
  - только `SELF_ONLY`.
- **Условие аудита, противоречащее нашей схеме.** В Intended Use сказано: «Not acceptable: A utility tool to help upload contents to the account(s) you or your team manages». Клиент «should be intended for a wide audience, not limited to internal groups/private use» ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines)).
- Лимиты:
  - около 15 постов в день на аккаунт через Direct Post, общий лимит для всех клиентов ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines));
  - 6 запросов в минуту на токен;
  - ошибки `spam_risk_too_many_posts` и `unaudited_client_can_only_post_to_private_accounts` ([Direct Post API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)).
- Поля `post_info` ([Direct Post API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)):
  - `privacy_level` — `PUBLIC_TO_EVERYONE`, `MUTUAL_FOLLOW_FRIENDS`, `FOLLOWER_OF_CREATOR` или `SELF_ONLY`; значение должно быть из `privacy_level_options`, которые возвращает `/creator_info/query/`;
  - `title`, `disable_duet`, `disable_stitch`, `disable_comment`, `video_cover_timestamp_ms`;
  - `brand_content_toggle` (обязательное), `brand_organic_toggle`, `is_aigc`.
  Поле `source_info.source` — `PULL_FROM_URL` или `FILE_UPLOAD`.
- Обязательный UX ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines)):
  - показывать nickname аккаунта;
  - проверять `max_video_post_duration_sec`;
  - privacy выбирается вручную из списка без значения по умолчанию;
  - Comment, Duet и Stitch по умолчанию выключены;
  - переключатель коммерческого раскрытия по умолчанию выключен; branded content нельзя сделать private;
  - показывать превью, давать редактировать подпись, не добавлять промо-водяные знаки;
  - отправлять видео только после «expressly consent» пользователя.
- Если файл уже лежит на сервере клиента, «do not use FILE_UPLOAD; use PULL_FROM_URL instead». Для этого домен или префикс URL нужно верифицировать ([sharing guidelines](https://developers.tiktok.com/doc/content-sharing-guidelines)).
- Альтернатива — Upload в inbox (`/v2/post/publish/inbox/video/init/`, scope `video.upload`): пользователь сам завершает публикацию в приложении по уведомлению ([Upload API](https://developers.tiktok.com/doc/content-posting-api-reference-upload-video)). Распространяется ли на этот путь ограничение неаудированных клиентов — `[не проверено]`.
- → Автопубликация в TikTok из hub, скорее всего, не пройдёт аудит. Реалистичный путь — загрузка в inbox, после чего человек вручную публикует ролик в приложении и там же ставит AI-метку и раскрытие.

## 9. Чек-лист «красные флаги» для ревьюера

| Паттерн в ролике | Последствие | Как исправить |
|---|---|---|
| Неподтверждённые данные о выборах или кризисе как факт ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | FYF ineligible; при «significant harm» удаление | Помечать «не подтверждено», ссылаться на официальный источник |
| Неверная информация о том, как, где и когда голосовать ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | Удаление, нарушение в account status | Сверка с избиркомом штата; при сомнении убрать |
| Pixel-art сцена выстрела, гибели или пытки реального человека ([sensitive](https://www.tiktok.com/community-guidelines/en/sensitive-mature-themes)) | Удаление; в news-контексте как минимум FYF ineligible и 18+ | Показывать место, последствия, хронологию — без момента насилия |
| Кровь или тела в первом кадре и на обложке ([sensitive](https://www.tiktok.com/community-guidelines/en/sensitive-mature-themes), [API](https://developers.tiktok.com/doc/content-posting-api-reference-direct-post)) | FYF ineligible, «Disturbing» для CRP | Нейтральный первый кадр, `video_cover_timestamp_ms` |
| Флаг, символ или лицо стрелка либо цитата манифеста без осуждения ([safety](https://www.tiktok.com/community-guidelines/en/safety-civility)) | Удаление | Не показывать или размыть; явная новостная и осуждающая рамка |
| Политик в pixel-art «совершает» то, чего не было, или «поддерживает» кого-то ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | Удаление (misleading AIGC) | Изображать только задокументированные действия и цитаты |
| Pixel-art «под» конкретное СМИ (логотип, плашка CNN или Fox) ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | Удаление («made to look like it comes from a real news source») | Только собственный брендинг 8bitnews |
| Узнаваемый несовершеннолетний (подозреваемый или жертва), в том числе в pixel-art ([AIGC help](https://support.tiktok.com/en/using-tiktok/creating-videos/ai-generated-content), [safety](https://www.tiktok.com/community-guidelines/en/safety-civility)) | Удаление | Не изображать и не называть; силуэт без черт |
| Фотореалистичная генерация или клон голоса реального человека без метки ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | Удаление, ограничение или метка от TikTok | `is_aigc=true`, а лучше не использовать |
| Насмешка над жертвами трагедии или над внешностью политика ([safety](https://www.tiktok.com/community-guidelines/en/safety-civility)) | Удаление | Критиковать действия, а не внешность |
| «Террористы/преступники» про целую этническую или религиозную группу ([safety](https://www.tiktok.com/community-guidelines/en/safety-civility)) | Удаление | Атрибутировать конкретным лицам |
| Однотипные ролики со статичными слайдами ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity), [CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)) | FYF ineligible, потеря CRP, ineligible аккаунт | Анимация, разные визуальные решения, авторский анализ |
| Одинаковые ролики в нескольких аккаунтах по разным странам ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | Spam: удаление или бан | Локализованный, разный контент для каждой страны |
| Кликбейтная подпись, не соответствующая ролику ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity)) | FYF ineligible, CRP «Clickbait» | Подпись = факт из ролика |
| Вшитая музыка без лицензии на композицию и запись ([MUC](https://www.tiktok.com/legal/page/global/music-usage-confirmation/en), [IP](https://www.tiktok.com/legal/page/global/copyright-policy/en)) | Удаление, повторы ведут к бану | Royalty-free с лицензией для TikTok или без музыки |
| Оплата от партии или кандидата за ролик ([integrity](https://www.tiktok.com/community-guidelines/en/integrity-authenticity), [BC](https://www.tiktok.com/legal/page/global/bc-policy/en)) | Удаление (paid political advertising) | Не брать политических заказов |
| Ролик ≤60 с при расчёте на CRP ([CRP](https://www.tiktok.com/creator-academy/article/creator-rewards-program/?lang=en)) | Нет вознаграждения | Для CRP-рынков формат >60 с, 1080p |
