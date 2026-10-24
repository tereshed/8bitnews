---
verified_at: 2026-10-24
purpose: База правил Instagram (Meta) для LLM-агентов, которые проверяют Reels 8bitnews на риск страйков, отключения аккаунта, демонетизации и потери рекомендаций.
---

# Instagram (Reels): правила площадки для ревью роликов

Обозначения: `[не проверено]` — не удалось подтвердить на официальной странице на дату `verified_at`;
`→` — вывод или рекомендация для нашей модели канала. Community Standards (CS) общие для Facebook, Instagram и Threads.

## 1. Санкции и апелляции

- Нарушивший контент удаляют и могут поставить страйк. Будет ли страйк, зависит от «severity of the content, the context in which it was shared and when it was posted» ([counting strikes](https://transparency.meta.com/enforcement/taking-action/counting-strikes/)).
- Страйк не ставят за контент старше 90 дней, а при тяжёлых нарушениях — старше 4 лет. Несколько удалений сразу могут засчитать как один страйк. «All strikes on Facebook or Instagram expire after one year» ([counting strikes](https://transparency.meta.com/enforcement/taking-action/counting-strikes/)).
- Шкала «1 страйк — warning … 10+ — 30 дней без публикаций» применяется только к Facebook-аккаунтам. Страйки при этом считаются и в Facebook, и в Instagram ([restricting accounts](https://transparency.meta.com/enforcement/taking-action/restricting-accounts/)).
- Отключение аккаунта ([disabling accounts](https://transparency.meta.com/enforcement/taking-action/disabling-accounts/)):
  - «After 5 strikes, you may receive additional 30-day restrictions from creating content, or we may remove your account»;
  - тяжёлые нарушения, например CSAM, ведут к отключению после одного случая;
  - аккаунты, созданные для обхода ограничений, отключают сразу.
- Страница disabling accounts (5 страйков) и страница restricting accounts (10+) дают разные пороги. Публичного порога именно для Instagram не найдено `[не проверено]`.
- История нарушений и ограничений видна в Account Status. Решение обжалуют в приложении. Если контент восстановили, страйк и ограничение снимают ([restricting accounts](https://transparency.meta.com/enforcement/taking-action/restricting-accounts/)).
- Отключённый аккаунт можно обжаловать при входе в Instagram ([disabled accounts](https://help.instagram.com/366993040048856)).
- Повторные нарушения прав на интеллектуальную собственность ведут к отключению по repeat infringer policy ([repeat infringer](https://help.instagram.com/1586774981367195)).
- Аккаунты, которые недавно нарушали правила, не рекомендуются тем, кто на них не подписан ([recommendations](https://help.instagram.com/313829416281232)).

## 2. Community Standards для новостного контента

**Дезинформация и выборы** ([misinformation](https://transparency.meta.com/policies/community-standards/misinformation/))
- Удаляют дезинформацию, которая ведёт к физическому вреду или к «interference with the functioning of political processes».
- Voter or Census Interference удаляют. Это ложь:
  - о датах, местах, времени и способах голосования и регистрации;
  - о том, кто может голосовать и будет ли голос учтён;
  - о том, участвует ли кандидат в выборах;
  - «False or unverified claims that… ICE is at a voting location»;
  - об условиях на избирательном участке, из-за которых голосовать якобы невозможно.
- Остальную дезинформацию не удаляют, а ограничивают: в США работает Community Notes, вне США — сторонние фактчекеры. Аккаунты, которые повторно распространяют такую дезинформацию, получают «decreased distribution».
- Manipulated media: на фотореалистичный или реалистично звучащий цифровой контент, который «creates a particularly high risk of materially deceiving the public on a matter of public importance», Meta может поставить информационную метку.

**Насилие и графика** ([violent & graphic](https://transparency.meta.com/policies/community-standards/violent-graphic-content/), правки до 27.08.2026)
- Удаляют видео с расчленением, обнажёнными внутренними органами, сожжёнными людьми, перерезанием горла, а также садистские комментарии.
- Warning screen и 18+: «Imagery… depicting a persons' violent death (including their moment of death or the aftermath)», жестокость к связанным людям, казни (кроме прямых эфиров).
- Fictional imagery с насильственной смертью вымышленных людей — тоже warning screen и 18+.
- Только 18+: тела погибших, драки, горящие и разбитые машины, кровь и физиологические жидкости.
- Изображение насильственной смерти удаляют по запросу семьи, если смерть подтверждена.

**Опасные организации и лица** ([DOI](https://transparency.meta.com/policies/community-standards/dangerous-individuals-organizations/), правка 19.03.2026)
- Разрешены «reporting on, neutrally discussing or condemning» опасных организаций. Пример новостного формата: «Breaking News: Al-Shabab claimed responsibility…».
- При этом Meta «require[s] people to clearly indicate their intent… If a user's intention is ambiguous or unclear, we default to removing content».
- Tier 1 (терроризм, hate-организации, картели, массовые убийства): удаляют glorification, support и representation, неясные отсылки и символы.
- Удаляют также «perpetrator-generated content» и «third-party imagery depicting the moment of such attacks on visible victims». Цитата организации без осуждающей или новостной подписи считается support.

**Травля и узнаваемые люди** ([bullying & harassment](https://transparency.meta.com/policies/community-standards/bullying-harassment/), правка 23.09.2026)
- Public figures — должностные лица штата и страны, кандидаты на эти посты, люди с более чем 1 млн подписчиков и «people who receive substantial news coverage».
- О публичных фигурах можно больше критики. Тяжёлые атаки удаляют, а часть атак удаляют, если человек «directly tagged».
- Защита для всех (Tier 1):
  - «Derogatory sexualized photoshop or drawings»;
  - «Claims that a violent tragedy did not occur»;
  - утверждения, что жертвы «acting or pretending»;
  - призывы к самоповреждению.
- Для публичных фигур при «purposefully exposed» (например, если отметить их в посте) удаляют насмешки над смертью или болезнью и «statements of inferiority about physical appearance».
- Несовершеннолетние: «heightened protection for anyone under the age 18». Частных несовершеннолетних защищают даже от «Allegations about criminal or illegal behavior» (Tier 4).
- Отдельно защищены от массовой травли «Victims of violent events/tragedies» и «Election officials».

**Исключение для новостей (newsworthiness)**
- Применяется «in rare cases» после взвешивания общественного интереса и риска вреда. Если допущен чувствительный контент, ставят warning screen и 18+ ([newsworthy content](https://transparency.meta.com/features/approach-to-newsworthy-content/)).
- Ни один источник, включая СМИ и политиков, не считается newsworthy автоматически ([newsworthy content](https://transparency.meta.com/features/approach-to-newsworthy-content/)).
- → На исключение не рассчитываем: роль новости, цель и осуждение должны быть видны в кадре и в подписи.

## 3. Синтетический и AI-контент

- Обязательно раскрывать «photorealistic video or realistic-sounding audio that has been digitally generated or altered, including with AI» ([AI label](https://help.instagram.com/761121959519495)). Примеры, где метка нужна:
  - «**A reel narrated with a realistic AI-generated voiceover**»;
  - «A song created using AI-generated vocals».
- Метка не нужна для «Video of an outdoor landscape, created in a style resembling a cartoon» и для изображений. Изображения Meta помечает сама, если находит AI-сигналы ([AI label](https://help.instagram.com/761121959519495)).
- За отсутствие обязательной метки «there may be penalties» ([AI label](https://help.instagram.com/761121959519495), [misinformation](https://transparency.meta.com/policies/community-standards/misinformation/)).
- Meta сама ставит метку «AI info», если находит отраслевые сигналы, в том числе для сторонних AI-инструментов. Если сигнал говорит только о редактировании AI, информация уходит в меню поста ([AI label](https://help.instagram.com/761121959519495), [labeling AI content](https://transparency.meta.com/governance/tracking-impact/labeling-ai-content)).
- Аккаунты, которые регулярно публикуют фотореалистичных AI-людей без метки «AI-generated profile», не рекомендуются ([recommendations](https://help.instagram.com/313829416281232)).
- **API:** параметр `is_ai_generated` (true/false) при создании контейнера. Описание: «An optional parameter to provide a self-disclosure of AI usage in the post». Работает при входе через Facebook Login и через Instagram Login. Для детей карусели не задаётся ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/), [IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- → Наш TTS-голос — «realistic AI-generated voiceover», поэтому в Instagram **для каждого ролика ставим `is_ai_generated=true`**. Pixel-art сам по себе метки не требует.

## 4. Монетизация и право на рекомендации

- Recommendation Guidelines строже CS. Low-quality контент не рекомендуется ([recommendations](https://help.instagram.com/313829416281232)):
  - «unoriginal content», то есть в основном чужой контент с незначительными изменениями;
  - длинные нерелевантные подписи и организованные комментарии для накрутки охвата;
  - контент, оценённый фактчекерами как ложный.
  В той же группе — контент с насилием, например драками: подросткам его не показывают.
- Оригинальный контент получает «the best reach and distribution» ([original content](https://help.instagram.com/1800814370401535)).
  - Unoriginal: контент, который аккаунт «did not film or produce», с поверхностными правками — «purely descriptive» подписями, субтитрами на языке оригинала, рамками, логотипами.
  - Материальная правка: «text or voice narration that appears throughout and adds new information».
- → Наш ролик собран из собственной генерации, так что формально он оригинален. Риск появляется, если в кадр попадают чужие видео или скриншоты без нового смысла.
- Partner Monetization Policies ([partner monetization](https://help.instagram.com/512371932629820)):
  - нужно жить в стране, где функция доступна;
  - соблюдать CS и Content Monetization Policies;
  - за misinformation и false news можно потерять право на монетизацию;
  - нужна «authentic, established presence»;
  - «Communications that are regulated as political advertising» не монетизируются;
  - политики, кандидаты, партии и госорганы не монетизируются.
- Content Monetization Policies (restricted/prohibited categories) существуют, но содержимое разделов на странице не загрузилось `[не проверено]` ([CMP](https://www.facebook.com/business/help/1348682518563619)).
- Spam: «Posting… either manually or automatically, at very high frequencies» запрещено. При меньшей частоте возможны ограничения, если есть «posting repetitive content» ([spam](https://transparency.meta.com/policies/community-standards/spam/)).

## 5. Политический контент

- С 18.09.2025 Meta относится к политическому контенту «more like other types of content» и может рекомендовать его людям, которые не подписаны на аккаунт, в Reels, Explore и In-Feed Recommendations. У пользователей есть настройки, сколько политики видеть ([political content](https://transparency.meta.com/features/approach-to-political-content/)).
- Значение этой настройки по умолчанию в Instagram на 2026-10-24 не подтверждено `[не проверено]`.
- Реклама о социальных вопросах, выборах и политике (SIEP) ([SIEP](https://transparency.meta.com/policies/ad-standards/SIEP-advertising/SIEP)):
  - нужна авторизация и «Paid for by»; исключение — «news publishers identified by Meta»;
  - **в ЕС такую рекламу показывать нельзя** (важно для Испании);
  - в США реклама не должна отговаривать от голосования, ставить под сомнение легитимность выборов или преждевременно объявлять победу;
  - AI в фотореалистичной SIEP-рекламе нужно раскрывать.
  → Продвижение (boost) наших новостных Reels — это SIEP-реклама.
- Перед промежуточными выборами 2026 года Meta блокирует новую политическую рекламу в последнюю неделю кампании. Для органического контента остаются в силе правила о voter interference и метка «AI info» ([Meta newsroom](https://about.fb.com/news/2026/02/meta-prepares-for-2026-us-midterms/)). Это блог, дополнение к официальным политикам.
- Branded content нельзя использовать для «Controversial political or social issues or crises in an exploitative manner for commercial purposes» и для контента, нарушающего правила SIEP. Партнёрам из категории «Government, elections or politics» нужна предварительная авторизация Meta ([branded content](https://help.instagram.com/1695974997209192)).
- State-controlled media — СМИ «partially or wholly under the editorial control of their government». Метка стоит в профиле и в About this Account. Публичные СМИ с редакционной независимостью метку не получают ([state-controlled media](https://help.instagram.com/2589432474704452)).

## 6. Копирайт и музыка

- Licensed Music Library «is intended for personal, non-commercial use». У части бизнес-аккаунтов и типов постов доступа к ней нет. Альтернатива — Sound Collection: «over 14,000 songs and sounds which are entirely royalty free», их можно использовать и в рекламе ([licensed music](https://www.facebook.com/business/help/402084904469945)).
- Почему звук заглушили: нет доступа к Licensed Music Library, изменились лицензии или «The rights holder blocked your content because it contains content they own». Заглушённый пост остаётся доступен ([audio problems](https://help.instagram.com/1612770602842742)).
- Если в загруженном видео распознан лицензированный трек, атрибуция автоматически меняется с Original Audio на артиста. Original audio публичного Reel другие пользователи могут переиспользовать ([reel audio](https://help.instagram.com/329208821595430)).
- В API «Music tagging is only available for original audio». Название аудио задаёт `audio_name`, переименовать его можно один раз ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- → Аккаунт для API будет professional, поэтому вшитая музыка должна быть royalty-free с правом коммерческого использования. Коммерческие треки рискуют заглушением и страйками по repeat infringer policy.

## 7. Метаданные: подписи, обложки, хэштеги

- Подпись (`caption`): до 2200 символов, 30 хэштегов и 20 упоминаний. Отмеченные пользователи получают уведомление ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- → Не отмечаем через @ политиков в критических роликах: по B&H отметка — один из сигналов «purposefully exposed» ([bullying & harassment](https://transparency.meta.com/policies/community-standards/bullying-harassment/)).
- Длинные нерелевантные подписи и организованные комментарии исключают пост из рекомендаций ([recommendations](https://help.instagram.com/313829416281232)).
- Spam-запреты ([spam](https://transparency.meta.com/policies/community-standards/spam/)):
  - обмен вовлечённости на ценности;
  - like/share-gating;
  - «Misleading Links» — ссылки, которые обещают одно, а ведут на другое.
- Обложка задаётся через `cover_url` (JPEG, рекомендуется 9:16; в ленте обрезается до 1:1) или через `thumb_offset` в мс. Если заданы оба, используется `cover_url` ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- Отдельного правила Meta о кликбейтных обложках Reels в Instagram не найдено `[не проверено]`. Действует общий запрет вводящих в заблуждение ссылок из Spam.

## 8. Ограничения Instagram API

- Публиковать можно только в professional-аккаунты Instagram. Права: `instagram_business_content_publish` при входе через Instagram или `instagram_content_publish` при входе через Facebook Login ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/)).
- Если связанная Страница требует Page Publishing Authorization, публикация невозможна, пока её не пройти ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/)).
- Лимиты ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/), [IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)):
  - «100 API-published posts within a 24-hour moving period», проверка через `GET /<IG_ID>/content_publishing_limit`; в разделе о каруселях той же страницы указано 50 — документация противоречит сама себе;
  - не более 400 контейнеров за 24 часа; неопубликованный контейнер истекает через 24 часа.
- Медиафайл должен лежать на публичном сервере, Meta скачивает его по URL. Для больших файлов есть resumable upload через `rupload.facebook.com` ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/)).
- Требования к Reels ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)):
  - контейнер MP4 или MOV, видео H.264 или HEVC, звук AAC до 48 кГц;
  - 23–60 fps, длительность от 3 с до 15 мин, размер до 300 МБ;
  - рекомендуемое соотношение 9:16.
- Параметры Reels ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)):
  - `media_type=REELS`, `video_url`, `caption`, `share_to_feed`;
  - `collaborators` (до 3), `cover_url`, `audio_name`, `user_tags`, `location_id`, `thumb_offset`;
  - `trial_params`, `is_ai_generated`.
- Партнёрская метка — `branded_content_sponsor_ids` и `is_paid_partnership`, только при входе через Facebook Login ([content publishing](https://developers.facebook.com/docs/instagram-platform/content-publishing/)).
- Настройки приватности аккаунта, например remixing, применяются к публикуемому ролику ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- Отдельного поля для политического контента нет; для AI есть только `is_ai_generated` ([IG User Media](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/media)).
- Требования App Review и Advanced Access для нашего сценария (свой аккаунт, автоматическая публикация) отдельно не проверены `[не проверено]`.

## 9. Чек-лист «красные флаги» для ревьюера

| Паттерн в ролике | Последствие | Как исправить |
|---|---|---|
| TTS-озвучка без AI-метки ([AI label](https://help.instagram.com/761121959519495)) | Возможны санкции, метка от Meta | `is_ai_generated=true` всегда |
| Ложь о дате, месте или способе голосования, «ICE на участке» ([misinformation](https://transparency.meta.com/policies/community-standards/misinformation/)) | Удаление и страйк | Только подтверждённые данные избиркома |
| Неподтверждённая или ложная новость ([misinformation](https://transparency.meta.com/policies/community-standards/misinformation/), [recommendations](https://help.instagram.com/313829416281232)) | Community Notes или метка фактчекеров, нет рекомендаций, потеря монетизации | Разделять «известно» и «не подтверждено», давать источники |
| Упоминание организации, лидера или символа из DOI без явной новостной или осуждающей рамки ([DOI](https://transparency.meta.com/policies/community-standards/dangerous-individuals-organizations/)) | Удаление (неясное намерение) и страйк | Явная формулировка «сообщают / осуждают» в кадре и подписи |
| Pixel-art момента атаки с видимыми жертвами ([DOI](https://transparency.meta.com/policies/community-standards/dangerous-individuals-organizations/)) | Удаление (вероятно, как «imagery depicting the moment») | Показывать место и последствия без жертв |
| Сцена насильственной смерти реального человека, даже стилизованная ([violent & graphic](https://transparency.meta.com/policies/community-standards/violent-graphic-content/)) | Warning screen, 18+, нет рекомендаций | Без момента смерти; символическая графика |
| «Унизительный» рисунок политика, сексуализированный образ ([B&H](https://transparency.meta.com/policies/community-standards/bullying-harassment/)) | Удаление | Без сексуализации и насмешек над внешностью |
| Отметка политика через @ в критическом ролике ([B&H](https://transparency.meta.com/policies/community-standards/bullying-harassment/)) | Повышает риск удаления по B&H | Не отмечать, писать имя текстом |
| Отрицание или «постановка» трагедии, сомнение в жертвах ([B&H](https://transparency.meta.com/policies/community-standards/bullying-harassment/)) | Удаление | Не публиковать такие версии без опровержения |
| Несовершеннолетний подозреваемый узнаваем: имя или черты ([B&H](https://transparency.meta.com/policies/community-standards/bullying-harassment/)) | Удаление (Tier 4) | Не называть и не изображать узнаваемо |
| Чужие клипы или скриншоты без новой информации ([original content](https://help.instagram.com/1800814370401535)) | Unoriginal, нет рекомендаций | Собственная графика, голос с новыми фактами и анализом |
| Коммерческий трек вшит в ролик бизнес-аккаунта ([licensed music](https://www.facebook.com/business/help/402084904469945), [audio](https://help.instagram.com/1612770602842742)) | Заглушение звука, IP-удаление, repeat infringer | Sound Collection или royalty-free с коммерческой лицензией |
| Boost ролика о выборах или политике ([SIEP](https://transparency.meta.com/policies/ad-standards/SIEP-advertising/SIEP)) | Отказ без авторизации; в ЕС запрещено | Не продвигать политические ролики платно |
| Оплата от партии или кандидата за ролик ([branded content](https://help.instagram.com/1695974997209192), [partner monetization](https://help.instagram.com/512371932629820)) | Нарушение Branded Content и SIEP, потеря монетизации | Не брать; иначе авторизация и paid partnership |
| Одинаковые ролики с высокой частотой ([spam](https://transparency.meta.com/policies/community-standards/spam/)) | Ограничения аккаунта | 1–3 разных ролика в день, без дублей |
