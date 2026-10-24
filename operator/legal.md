# Оператор — правовая база для Legal (Испания, ЕС; США для иностранного оператора)

verified_at: 2026-10-24

> **Это не юридическая и не налоговая консультация.** Это рабочая база для агентов Legal и для модератора. Её нужно проверить у юриста и gestor в Испании и у медиаюриста в США, а затем перепроверять при смене формата, доходов или состава команды.
> Оператор — физическое лицо, живёт в Испании, не из США; деньги проекта тоже не из США. Файл действует для **всех** каналов, включая американский. Право контента США — в [`countries/us/legal.md`](../countries/us/legal.md), редакционные правила — в [`countries/us/policy.md`](../countries/us/policy.md), площадки — в `platforms/*/rules.md`. Здесь их содержание не повторяется. Если правила расходятся, действует более строгое.
> Пометки: `[не проверено]` — нет подтверждения в первичном источнике; `[вторичный]` — обзор юрфирмы, СМИ, трекер; `[ссылка не открыта]` — адрес не открылся роботу или собран по шаблону; `[толкование]` — наш вывод из текста, позиции органа или суда нет.
> Как сверяли (24.10.2026): EUR-Lex закрыт от роботов, поэтому тексты актов ЕС сверены по тем же файлам OJ в Publications Office (cellar). Ссылки ELI даны как канонические. Статьи BOE сверены через открытый API BOE (`datosabiertos`), ссылки `act.php` ведут на тот же консолидированный текст.
> Ссылка на раздел: `operator/legal.md#<id>`.

## <a id="scope"></a>0. Кто отвечает и какое право применяется
- Отвечает оператор лично: он deployer AI-систем, controller персональных данных, «prestador» по аудиовизуальному праву, а при иске — ответчик по месту жительства. Человек-модератор вину с оператора не снимает.
- **Иск в Испании возможен по любому каналу.** Лицо с домицилием в государстве ЕС судят там «sea cual sea su nacionalidad». Кроме того, иск можно подать по месту, где произошло вредное событие ([Brussels I bis, ст. 4.1, 7.2](https://www.boe.es/doue/2012/351/L00001-00032.pdf)).
  - Rome II диффамацию исключает (ст. 1.2(g), [текст](https://www.boe.es/buscar/doc.php?id=DOUE-L-2007-81334)). Поэтому испанский суд применит Código Civil, ст. 10.9: «la ley del lugar donde hubiere ocurrido el hecho de que deriven» ([CC](https://www.boe.es/buscar/act.php?id=BOE-A-1889-4763)). Для ролика, выпущенного из Испании, это может оказаться испанское право — даже если ролик на английском и о США `[толкование]`.
- Уголовная юрисдикция Испании распространяется на «delitos … cometidos en territorio español» ([LOPJ, ст. 23.1](https://www.boe.es/buscar/act.php?id=BOE-A-1985-12666)). Считается ли так публикация, которую оператор делает из Испании, — `[толкование]`; исходим из того, что считается.

| Норма | Канал US | Канал ES |
|---|---|---|
| AI Act, ст. 50 ([#ai-act-art50](#ai-act-art50)) | да: оператор в ЕС | да |
| GDPR, LOPDGDD, LSSI ([#gdpr-role](#gdpr-role), [#lssi](#lssi)) | да: досье о людях США тоже | да |
| LGCA, «usuario de especial relevancia» ([#lgca-thresholds](#lgca-thresholds)) | да: доходы и подписчики суммируются | да |
| LO 1/1982, LO 2/1984, Código Penal ([#es-honor](#es-honor), [#es-criminal](#es-criminal)) | при иске или жалобе в Испании | да |
| LOREG ([#es-elections](#es-elections)) | только если канал касается выборов в Испании | да |
| FEC, FARA ([#us-fec-foreign-press](#us-fec-foreign-press), [#us-fara](#us-fara)) | да | нет `[толкование]` |
| Налоги Испании; удержание налога в США ([#es-tax](#es-tax), [#us-tax-withholding](#us-tax-withholding)) | да | да: доход от зрителей из США |

## 1. EU AI Act (Regulation (EU) 2024/1689)

### <a id="ai-act-art50"></a>1.1 Ст. 50: обязанности deployer
- Deployer — тот, кто использует AI-систему «under its authority», кроме «personal non-professional activity» (ст. 3(4)). Guidelines Комиссии, п. 19: «Any activity through which natural persons gain an economic benefit on a regular basis … should be considered as a 'professional' activity». Монетизируемые каналы — профессиональная деятельность, значит оператор — deployer ([AI Act](http://data.europa.eu/eli/reg/2024/1689/oj); [Guidelines](https://ec.europa.eu/newsroom/dae/redirection/document/131215)).
- **Deep fakes**, ст. 50(4), абз. 1: deployer «shall disclose that the content has been artificially generated or manipulated». Deep fake — изображение, аудио или видео, которое «resembles existing persons … and would falsely appear to a person to be authentic or truthful» (ст. 3(60)).
  - Мягкий режим для «evidently artistic, creative, satirical, fictional» работ к новостям неприменим. Guidelines, п. 122, исключают контент «exclusively informative … (e.g. news reporting)».
- **AI-текст**, ст. 50(4), абз. 2: текст, «published with the purpose of informing the public on matters of public interest», раскрывается как AI. Исключение: контент «has undergone a process of human review or editorial control and … a natural or legal person holds editorial responsibility».
- **Как раскрывать**, ст. 50(5): «in a clear and distinguishable manner at the latest at the time of the first interaction or exposure», с учётом требований доступности.
- Провайдеры генераторов обязаны ставить машиночитаемую маркировку (ст. 50(2)). Это обязанность провайдера, но наш вывод тоже касается: водяные знаки и C2PA генераторов не удаляем.

### <a id="ai-act-our-case"></a>1.2 Наш случай: что считается deep fake и подходит ли исключение
- **Pixel-art реальных людей.** Guidelines, п. 114: «photorealism alone is not determinative», контент оценивается целиком, с учётом контекста. Пример «не deep fake» — «AI-generated cartoon of a pre-existing image depicting a historical event». Пример deep fake — «AI-generated video of an individual resembling a politician, holding a speech». Явный pixel-art, скорее всего, не deep fake, но сцена, где политик «говорит», ближе ко второму примеру `[толкование]`.
- **Синтетический голос диктора.** Под «persons» понимаются и «realistic AI-generated human avatars or personas … voice», если субъект «can plausibly exist» (п. 113). Не deep fake — «AI voice replication for fictional characters … when there is no deception as to the identity of the narrators». Поэтому голос диктора прямо называем AI-голосом.
- **Сценарий, прочитанный голосом.** Является ли он «text» по ст. 50(4), источники не говорят. Текст в кадре и описание — скорее «text» (Guidelines, п. 60: «capable of being read») `[толкование]`.
- **Подходит ли исключение human review.** По Guidelines (п. 134–138):
  - «Fact-checking the accuracy of the content is a minimum requirement»;
  - «cursory editorial approval» не засчитывается;
  - существенная правка AI после одобрения лишает исключения;
  - контакт лица с editorial responsibility нужно опубликовать.
  Одна кнопка «Опубликовать» в Telegram под это может не подойти: фактчек у нас делает LLM (`fact_checker`, QA), а не человек `[толкование]`.
- **Вывод.** Правило policy.md «помечаем всё» закрывает ст. 50 проще, чем ставка на исключение. Если нужно исключение для текста: у модератора чек-лист сверки с досье и журнал проверок; после одобрения ролик не меняется; на странице канала опубликован контакт editorial responsibility.

### <a id="ai-act-dates"></a>1.3 Сроки и Digital Omnibus
- Ст. 50 применяется с **02.08.2026** (ст. 113). Глава о штрафах (в том числе ст. 99) — с 02.08.2025 ([AI Act](http://data.europa.eu/eli/reg/2024/1689/oj)).
- Digital Omnibus принят: **Regulation (EU) 2026/1744 от 08.07.2026**, OJ L 24.07.2026, в силе с 27.07.2026 ([ELI](http://data.europa.eu/eli/reg/2026/1744/oj); [Hunton](https://www.hunton.com/privacy-and-cybersecurity-law-blog/eu-digital-omnibus-on-ai-enters-into-force) `[вторичный]`).
  - Ст. 50(1), (3)–(6) Omnibus не изменил: **обязанности deployer по 50(4) действуют с 02.08.2026, без отсрочки**.
  - Новая ст. 111(4): провайдеры генеративных систем, выпущенных на рынок до 02.08.2026, должны выполнить ст. 50(2) к 02.12.2026. AESIA даёт тот же срок для маркировки «hasta el 2/12/2026» ([AESIA](https://aesia.digital.gob.es/storage/media/normas-de-transparencia-datos-clave.pdf)).
  - Прочее, нам не нужное: high-risk по Annex III — с 02.12.2027, по Annex I — с 02.08.2028.

### <a id="ai-act-code"></a>1.4 Code of Practice и Guidelines
- Code of Practice on Transparency of AI-Generated Content: финальная версия — 10.06.2026 ([страница](https://digital-strategy.ec.europa.eu/en/policies/code-practice-ai-generated-content); [PDF](https://ec.europa.eu/newsroom/dae/redirection/document/129555)). 08.07.2026 Комиссия признала, что он «adequately covers» ст. 50(2), (4), (5) ([opinion](https://digital-strategy.ec.europa.eu/en/library/commission-opinion-assessment-code-practice-transparency-ai-generated-content)). Соблюдать Code добровольно, но это самый ясный способ показать соответствие.
- Что Code предлагает deployer:
  - EU-иконка или метка на основе «the capitalised acronym 'AI'» (варианты «AI GENERATED», «AI MODIFIED»);
  - видео — метка «at the beginning of the video as well as, where possible, at regular intervals», пример места — «top right corner»; аудио — «a short audible disclaimer» в начале; текст — «near the headline»;
  - deployer, который не media service provider, ведёт политику с указанием лица с editorial responsibility («name, role and contact details») и публикует контакты (Commitment 4).
- Guidelines по ст. 50 — C(2026) 5054 от 20.07.2026 ([страница](https://digital-strategy.ec.europa.eu/en/library/guidelines-transparency-obligations-providers-and-deployers-ai-systems)). Применимыми они станут после формального принятия на всех языках; принятие на 24.10.2026 `[не проверено]`.
- Для нас: к плашке «AI illustration» добавить метку «AI» в начале ролика (лучше по образцу Code) и голосовое или текстовое «AI voice» в начале. Согласовать с `platforms/*/rules.md` (безопасные зоны интерфейса).

### <a id="ai-act-fines"></a>1.5 Штрафы и надзор
- За нарушение ст. 50 — до €15 млн или до 3% мирового оборота, «whichever is higher» (ст. 99(4)(g)); для SME и стартапов — меньшая из сумм (ст. 99(6)). Считается ли физлицо «undertaking» для процентной части — `[не проверено]`.
- Надзор в Испании — AESIA; в списке Комиссии назначение помечено «pending final adoption» ([MSA](https://digital-strategy.ec.europa.eu/en/policies/market-surveillance-authorities-under-ai-act)). Национального закона о санкциях нет ([#es-ai-law](#es-ai-law)). Может ли AESIA штрафовать без него — `[толкование]`; риск считаем реальным с 02.08.2026.

### <a id="ai-act-scope"></a>1.6 Американский канал
- AI Act применяется к «deployers of AI systems that have their place of establishment or are located within the Union» (ст. 2(1)(b)). Условие «output used in the Union» есть только в ст. 2(1)(c) — для операторов из третьих стран. Значит, оператор в Испании подпадает под ст. 50 и с каналом для США (Guidelines, п. 13) `[толкование: про аудиторию только из США источник прямо не говорит]`.

## 2. Испания: закон об AI и AESIA

### <a id="es-ai-law"></a>2.1 Законопроект «buen uso y gobernanza de la IA»
- Anteproyecto одобрен 11.03.2025. Нарушение маркировки deep fakes в нём — infracción grave: «entre 500.000 euros y 7,5 millones … o entre el 1% y el 2%» ([MTDFP](https://digital.gob.es/comunicacion/notas-prensa/mtdfp/2025/03/2025-03-11)).
- Proyecto de Ley Orgánica одобрен Советом министров 26.05.2026 ([La Moncloa](https://www.lamoncloa.gob.es/consejodeministros/resumenes/paginas/2026/260526-rueda-prensa-ministros.aspx)), внесён как 121/000096 ([BOCG-15-A-97-1](https://www.congreso.es/public_oficiales/L15/CONG/BOCG/A/BOCG-15-A-97-1.PDF)):
  - ст. 21.1.b: нарушение ст. 50.4 deployer («ultrasuplantación» или текст об общественно значимом) — infracción grave;
  - ст. 30.1.c: штраф «hasta 7.500.000 euros o … hasta el 1 %», для SME — меньшая из сумм (ст. 30.8);
  - надзор за ст. 50 — AESIA (ст. 5.2.h).
- **Статус на 24.10.2026 — «Concluido - (Caducado)» с 06.10.2026** ([Congreso](https://www.congreso.es/es/busqueda-de-iniciativas?p_p_id=iniciativas&p_p_lifecycle=0&p_p_state=normal&p_p_mode=view&_iniciativas_mode=mostrarDetalle&_iniciativas_legislatura=XV&_iniciativas_id=121%2F000096)): Cortes распущены (RD 806/2026, [JEC](https://www.juntaelectoralcentral.es/cs/jec/eleccionesEnCurso/Elecciones_generales_2026)). Закона нет. Новые Cortes могут внести проект заново — следим.

### <a id="aesia"></a>2.2 AESIA
- Создана RD 729/2023; «sede institucional en la ciudad de A Coruña»; по статуту (ст. 4) ведёт «inspección, comprobación, sanción» в пределах, которые даёт право ЕС ([BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2023-18911)). Публикует разъяснения по ст. 50 ([инфографика](https://aesia.digital.gob.es/storage/media/normas-de-transparencia-datos-clave.pdf)).

## 3. Испания: Ley 13/2022 (LGCA) и «usuarios de especial relevancia»

### <a id="lgca-thresholds"></a>3.1 Пороги (RD 444/2024)
- Все условия должны выполняться **одновременно** (ст. 94.2 LGCA, ст. 2.1 и 4 RD) ([LGCA](https://www.boe.es/buscar/act.php?id=BOE-A-2022-11311); [RD 444/2024](https://www.boe.es/buscar/act.php?id=BOE-A-2024-8716)):
  - валовой доход **≥ €300 000** за прошлый календарный год, «derivados exclusivamente de la actividad … en el conjunto de los servicios»: реклама, спонсорство (в том числе натурой), выплаты площадок, платежи фанатов (ст. 3 RD);
  - **≥ 1 000 000 подписчиков на одной площадке или ≥ 2 000 000 суммарно** в любой момент прошлого года;
  - **≥ 24 ролика** за прошлый год, любой длины.
- Условие ст. 94.2.e — провайдер «establecido en España». Это так, если «tiene su sede central en España y las decisiones editoriales … se toman en España» (ст. 3.2.a). Ни язык, ни аудитория значения не имеют, и счёт не ограничен Испанией. Американский канал оператора тоже считается `[толкование; решения CNMC нет]`.

### <a id="lgca-duties"></a>3.2 Обязанности после порогов
- Зарегистрироваться в Registro Estatal (ст. 39 LGCA, ст. 94.4). Ведёт его министерство, а не CNMC (RD 1138/2023, ст. 4.2, 9.1.d). Срок — «en el plazo máximo de un mes desde el inicio de la actividad» (ст. 22.1.c) ([RD 1138/2023](https://www.boe.es/buscar/act.php?id=BOE-A-2023-25886)). Что считать «inicio» для того, кто пересёк порог, неясно `[не проверено]`.
- По ст. 94.1 применяются:
  - принципы ст. 4, 6, 7.1, 10, 12, 14, 15. Ст. 4.3 — уважать «el honor, la intimidad y la propia imagen» и обеспечивать «derechos de rectificación y réplica»; ст. 4.2 запрещает разжигание ненависти, в том числе по «opiniones políticas»;
  - защита несовершеннолетних, ст. 99.1 и 99.4: предупреждения, дескрипторы, символы возрастной классификации;
  - коммерческие коммуникации, ст. 121–134, когда оператор сам продаёт или организует рекламу.
- CNMC: реклама опознаётся, только если в самом видео есть «términos como 'publicidad' o 'publi'» ([CNMC, 03.06.2026](https://www.cnmc.es/prensa/requerimientos-influencer-20260603)).

### <a id="lgca-fines"></a>3.3 Штрафы
- Месяц подряд не выполнять обязанности ст. 94 — infracción grave (ст. 158.31); прочие нарушения ст. 94 — leve (ст. 159.9). Сроки давности — 3, 2 и 1 год (ст. 161).
- Ст. 160 «usuarios de especial relevancia» не называет. Для услуг с доходом < €2 млн: muy grave — до €60 000, grave — до €30 000, leve — до €10 000. Какая шкала применяется к нашей категории — `[толкование]`.
- Первые санкции CNMC (23.07.2026, все по ст. 158.31): Ibai Llanos — €710 за неопознанную рекламу, Jordi Wild — €1 543,44 за отсутствие предупреждения для несовершеннолетних (ст. 99.1, 99.4), Nachter — €1 779,94 за рекламу лекарств ([CNMC](https://www.cnmc.es/prensa/sanciones-influencers-20260723)).

### <a id="lgca-watch"></a>3.4 Что отслеживать заранее
- hub и DataLens считают по **оператору** (все каналы и площадки вместе): доход за календарный год, подписчиков на каждой площадке и суммарно, число роликов. 24 ролика мы наберём за недели, так что реальные триггеры — €300 000 и 1–2 млн подписчиков.
- Сигнал владельцу — заранее, например при 70% любого порога. Регистрация и метки возраста по ст. 99.1 должны быть готовы до пересечения порога.
- Законопроект о защите несовершеннолетних в цифровой среде (121/000052) расширял обязанности: «veracidad» для новостного контента, инструменты проверки возраста. Он caducado 06.10.2026 ([BOCG-15-A-52-1](https://www.congreso.es/public_oficiales/L15/CONG/BOCG/A/BOCG-15-A-52-1.PDF)) — может вернуться.

## 4. GDPR, LOPDGDD, LSSI

### <a id="gdpr-role"></a>4.1 Роль оператора и основание обработки
- GDPR применяется к обработке в контексте деятельности controller в ЕС — где бы ни стоял сервер и о ком бы ни были данные (ст. 3(1)). Досье о политиках США тоже под GDPR ([GDPR](https://www.boe.es/buscar/doc.php?id=DOUE-L-2016-80807); [ELI](http://data.europa.eu/eli/reg/2016/679/oj)).
- Основание для досье — legitimate interest (ст. 6(1)(f)). AEPD относит свободу информации к legitimate interest, а ст. 6(1)(e) к СМИ не применяет ([AEPD, PS/00335/2023](https://www.aepd.es/documento/reposicion-ps-00335-2023.pdf); [RR/00062/2023](https://www.aepd.es/es/documento/reposicion-ps-00193-2022.pdf)). Нужна письменная оценка legitimate interest.

### <a id="gdpr-journalism"></a>4.2 Журналистское исключение (ст. 85 GDPR) и Испания
- Ст. 85 обязывает государства согласовать защиту данных со свободой информации; в LOPDGDD общего журналистского исключения нет ([LOPDGDD](https://www.boe.es/buscar/act.php?id=BOE-A-2018-16673)). В уведомлении Комиссии по ст. 85(3) (04.08.2022) Испания написала: «No se ha considerado necesaria la aprobación de este tipo de normas … pues existe amplia y detallada jurisprudencia» ([Комиссия](https://commission.europa.eu/law/law-topic/data-protection/data-protection-eu/eu-member-states-notifications-european-commission-under-gdpr_en)).
- Баланс строится на ст. 20.1.d Конституции — право сообщать «información veraz por cualquier medio de difusión» ([CE](https://www.boe.es/buscar/act.php?id=BOE-A-1978-31229)) — и на практике AEPD: проверяется пропорциональность и «necesidad de publicar el concreto dato personal» ([RR/00062/2023](https://www.aepd.es/es/documento/reposicion-ps-00193-2022.pdf)).
- CJEU: журналистика — раскрытие публике «information, opinions or ideas, irrespective of the medium», и прибыль ей не мешает ([Satamedia, C-73/07, п. 59, 61](http://publications.europa.eu/resource/celex/62007CJ0073)). Публикация на YouTube «cannot in itself preclude» журналистскую цель, но не всё в интернете — журналистика ([Buivids, C-345/17, п. 56, 58](http://publications.europa.eu/resource/celex/62017CJ0345)). Подходит ли под это AI-конвейер — `[не проверено]`.

### <a id="gdpr-special"></a>4.3 Политические взгляды и уголовные данные
- Политические взгляды — особая категория (ст. 9). Основание — данные, «manifiestamente públicos» самим человеком (ст. 9(2)(e)): заявления политика, публичные позиции. Ст. 9(2)(g) в Испании требует «norma con rango de ley» (ст. 9.2 LOPDGDD), а для журналистики такого закона нет.
- Уголовные данные (ст. 10 GDPR; ст. 10 LOPDGDD): допустимы, только если обработку разрешает закон; ст. 10.3 называет лишь «abogados y procuradores». **Журналисты прямо не упомянуты.** Опора — ст. 85 GDPR и ст. 20 CE `[не проверено]`.
- Для нас:
  - в досье — только опубликованное официальными источниками и авторитетными СМИ, со ссылкой;
  - о частных лицах — минимум данных, без ID, адресов и фото;
  - обвиняемые, которые не являются публичными лицами, — по policy.md не называем, а значит и не храним по имени.

### <a id="gdpr-rights"></a>4.4 Права субъектов и наши правила
- Обязанность информировать (ст. 14): исключение — «esfuerzo desproporcionado», но тогда «haciendo pública la información» (ст. 14(5)(b)). Публичное уведомление о приватности на сайте источников, многоуровневое по ст. 11 LOPDGDD, — с категориями данных и «las fuentes».
- Удаление (ст. 17) не действует, если обработка нужна «para ejercer el derecho a la libertad de expresión e información» (ст. 17(3)(a)). На возражение (ст. 21) отвечаем, только показав «motivos legítimos imperiosos». Ответ — в течение месяца (ст. 12(3)).
- Хранение «no más tiempo del necesario» (ст. 5(1)(e)); после удаления данные блокируются на срок давности (ст. 32 LOPDGDD).
  - **Пересечение с policy.md («храним не меньше 2 лет»).** В Испании иск о honor «caducará» через 4 года (ст. 9.5 LO 1/1982), calumnias и injurias «prescriben al año» (ст. 131.1 CP, [CP](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444)). Для роликов о людях из Испании — обсудить с юристом срок 4 года и обосновать его защитой от исков.
- **LOPDGDD, ст. 86:** по мотивированному запросу СМИ добавляет «un aviso de actualización suficientemente visible», если новость больше не отражает положение человека — особенно когда уголовное дело решено в его пользу. Это совпадает с правилом US-файла «Обвинение снято → обновить описание и закреп» (`countries/us/legal.md#criminal-terms`). Распространяется ли норма на канал на YouTube («medio de comunicación digital» не определён) — `[не проверено]`.
- Ст. 85 LOPDGDD (для цифровых СМИ — «aviso aclaratorio» рядом с оригиналом после исправления), ст. 93 (удаление из поисковой выдачи), ст. 94 (соцсети) касаются в основном площадок и поисковиков.
- STC 58/2018: суд запретил индексацию имён во **внутреннем** поиске архива El País, а анонимизацию самой статьи признал излишним вмешательством ([BOE](https://www.boe.es/buscar/doc.php?id=BOE-A-2018-9534)). Для нас: страница источников ролика не ищется по имени частного лица.

### <a id="gdpr-records"></a>4.5 Реестр, подрядчики, передача в США
- Реестр обработки (ст. 30) обязателен: исключение для < 250 сотрудников не действует, если обработка «no sea ocasional» или включает данные по ст. 9 или 10 (ст. 30(5); ст. 31 LOPDGDD). Omnibus IV (2025/0130(COD)) это смягчит, но на 24.10.2026 не принят: пленарное голосование ожидается 23.11.2026 ([OEIL](https://oeil.europarl.europa.eu/oeil/en/procedure-file?reference=2025/0130%28COD%29)).
- С облаком, TTS, генераторами изображений и LLM — договоры processor (ст. 28).
- Передача в США: Data Privacy Framework действует. Иск Latombe отклонён (T-553/23, 03.09.2025, [CJEU](http://publications.europa.eu/resource/celex/62023TJ0553)), апелляция C-703/25 P на рассмотрении ([CJEU](http://publications.europa.eu/resource/celex/62025CN0703)). После Trump v. Slaughter EDPB 31.07.2026 попросил Комиссию оценить последствия для DPF ([EDPB](https://www.edpb.europa.eu/system/files/2026-08/edpb_letter_20260731_us_supremecourt_judgment_trump_v_slaughter_en.pdf)). Запасной вариант — SCC (Decision 2021/914). Лучше хранить данные в ЕС.

### <a id="lssi"></a>4.6 Страница источников: LSSI
- LSSI применяется к провайдерам с «residencia o domicilio» в Испании, в том числе без оплаты от пользователя, если сервис — «actividad económica para el prestador» ([LSSI](https://www.boe.es/buscar/act.php?id=BOE-A-2002-13758)). Сайт источников монетизируемых каналов — скорее экономическая деятельность `[толкование]`.
- Ст. 10.1 требует aviso legal: имя, «residencia o domicilio» (или постоянное заведение в Испании), email, NIF. Существенное нарушение 10.1 a/f — grave, €30 001–150 000 (ст. 38.3.b, 39). **Это раскрывает личность и адрес оператора** — решить с юристом до запуска.
- Cookies — только с согласием, кроме «estrictamente necesarias» (ст. 22.2; [гайд AEPD](https://www.aepd.es/guias/guia-cookies.pdf)); нарушение — leve (ст. 38.4.g). На странице источников — без аналитики и сторонних скриптов.

### <a id="gdpr-staff-viewers"></a>4.7 Модераторы и зрители
- Telegram — отдельный controller данных мессенджера; боты «completely independent from us» ([Telegram](https://telegram.org/privacy)). ID модераторов и журнал одобрений в hub — наши данные: уведомление по ст. 13, основание, срок хранения, запись в реестре.
- Если hub начнёт хранить комментарии зрителей — это персональные данные: уведомление, ст. 6(1)(f), право на возражение, срок хранения. Без этого комментарии не храним.

### <a id="gdpr-fines"></a>4.8 Штрафы и прецеденты AEPD
- GDPR: до €10 млн или 2% (ст. 83(4), в том числе ст. 30); до €20 млн или 4% (ст. 83(5): ст. 5, 6, 9, 12–22). Сроки давности по LOPDGDD: 3, 2, 1 год (ст. 72–74); отсутствие реестра — grave (ст. 73(n)).
- Неискажённый голос жертвы группового изнасилования из записи суда: ABC — €50 000 ([PS/00193/2022](https://www.aepd.es/es/documento/reposicion-ps-00193-2022.pdf)), Telecinco — €50 000 ([PS/00191/2022](https://www.aepd.es/documento/reposicion-ps-00191-2022.pdf)). Кадр частного лица из вирусного видео — El Español, €10 000 ([PS/00335/2023](https://www.aepd.es/documento/reposicion-ps-00335-2023.pdf)). Частное лицо за видео человека без согласия — €2 000 ([PS/00336/2023](https://www.aepd.es/documento/ps-00336-2023.pdf)).

## 5. Испания и ЕС: контент

### <a id="es-honor"></a>5.1 Honor, intimidad, propia imagen (LO 1/1982)
- Незаконное вмешательство (ст. 7) ([LO 1/1982](https://www.boe.es/buscar/act.php?id=BOE-A-1982-11196)):
  - 7.3 — огласка частной жизни;
  - 7.5 — публикация изображения человека;
  - 7.6 — «nombre, de la voz o de la imagen … para fines publicitarios, comerciales»;
  - 7.7 — факты или оценки, которые «lesionen la dignidad … menoscabando su fama».
- Разрешено (ст. 8.2): изображения публичных лиц на публичных мероприятиях; «la caricatura de dichas personas, de acuerdo con el uso social»; изображение, где человек «meramente accesoria». Pixel-art политика в новости ближе всего к caricatura, но выдумывать события она не позволяет `[толкование]`.
- Вред презюмируется, размер компенсации учитывает «la difusión o audiencia del medio» (ст. 9.3); срок на иск — 4 года (ст. 9.5).
- Свобода информации защищает только «información veraz». Это не абсолютная точность, а «un específico deber de diligencia» и «previo contraste con datos objetivos»; «simples rumores … meras invenciones» не защищены ([STC 6/1988, FJ 5](https://hj.tribunalconstitucional.es/es/Resolucion/Show/947)). Досье с источниками — доказательство такой проверки.
- Законопроект 121/000103 о новой редакции LO 1/1982 (одобрен 07.07.2026, [La Moncloa](https://www.lamoncloa.gob.es/consejodeministros/referencias/paginas/2026/20260707-referencia-rueda-de-prensa-ministros.aspx)): исключение «caricatura o parodia»; раскрытие «imágenes o voces extremadamente realistas … generadas»; согласие на изображение — с 16 лет. Caducado 06.10.2026 ([BOCG-15-A-104-1](https://www.congreso.es/public_oficiales/L15/CONG/BOCG/A/BOCG-15-A-104-1.PDF)).

### <a id="es-rectification"></a>5.2 Derecho de rectificación (LO 2/1984)
- Касается сведений, распространённых «por cualquier medio de comunicación social» (ст. 1). Сроки ([LO 2/1984](https://www.boe.es/buscar/act.php?id=BOE-A-1984-7248)):
  - заявитель направляет опровержение в течение 7 días naturales после публикации (ст. 2);
  - СМИ публикует его целиком, бесплатно и без комментариев «dentro de los tres días siguientes al de su recepción» (ст. 3);
  - иначе — иск в течение 7 días hábiles (ст. 4), заседание — в течение 7 дней (ст. 5).
- Суд в Мадриде (JPI nº 89, 23.09.2022) обязал интернет-СМИ опубликовать опровержение на сайте, YouTube и iVoox ([jotapov](https://jotapov.com/2022/09/28/otra-condena-mas-para-javier-negre-debera-rectificar-por-el-bulo-sobre-el-cierre-de-cuentas-de-ruben-sanchez/)) `[вторичный]`.
- **Испанские 3 дня — короче сроков CA и TX** (`countries/us/legal.md#defamation-retraction`). Для канала ES — SLA 3 дня с даты получения: решение модератора и юриста, публикация опровержения в описании, закрепе и при необходимости в ролике. Для канала US — то же, если заявитель в Испании `[толкование]`.
- Законопроект 121/000083 (20 días naturales для «usuarios de especial relevancia») — caducado 06.10.2026.

### <a id="es-criminal"></a>5.3 Código Penal
- **Calumnia** (ст. 205) — обвинение в преступлении «con conocimiento de su falsedad o temerario desprecio hacia la verdad». Публично — prisión от 6 месяцев до 2 лет или multa 12–24 месяца (ст. 206); доказанная правда освобождает (ст. 207). **Injuria** (ст. 208) — только тяжкая; публично — multa 6–14 месяцев (ст. 209). Об официальных лицах и их службе правда тоже защищает (ст. 210) ([CP](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444)).
- Владелец СМИ солидарно несёт гражданскую ответственность (ст. 212); нужна querella потерпевшего, кроме случаев с должностными лицами (ст. 215); добровольное опровержение снижает наказание (ст. 214); приговор публикуется за счёт виновного (ст. 216).
- **Корона**:
  - calumnia или injuria против короля и семьи в связи с их функциями — prisión от 6 месяцев до 2 лет или multa (ст. 490.3);
  - использование их образа «de cualquier forma que pueda dañar el prestigio de la Corona» — multa 6–24 месяца (ст. 491.2). **Это прямо касается pixel-art.**
  - Законопроект Sumar об отмене этих составов (122/000006) caducado 06.10.2026.
- Институты государства (ст. 504) — multa 12–18 месяцев. Hate speech (ст. 510) — prisión 1–4 года, строже при распространении через интернет. Enaltecimiento del terrorismo (ст. 578) — prisión 1–3 года, строже онлайн. Это важно для роликов о конфликтах.

### <a id="es-minors"></a>5.4 Несовершеннолетние
- Любое использование имени или изображения несовершеннолетнего, вредное его репутации, — вмешательство «incluso si consta el consentimiento del menor o de sus representantes legales»; Ministerio Fiscal действует сам (LO 1/1996, ст. 4.2–4.4, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-1996-1069)).
- В делах о несовершеннолетних «en ningún caso» СМИ не получают и не распространяют изображения и данные, позволяющие их опознать (LO 5/2000, ст. 35.2, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2000-641)). LGCA, ст. 95.2, запрещает идентифицировать несовершеннолетних в уголовном контексте; нарушение — muy grave (ст. 157.9). Policy.md уже запрещает их называть и изображать.

### <a id="es-elections"></a>5.5 Выборы (LOREG) — всеобщие выборы 29.11.2026
- Выборы назначены RD 806/2026 на **29.11.2026** ([JEC](https://www.juntaelectoralcentral.es/cs/jec/eleccionesEnCurso/Elecciones_generales_2026)); кампания — 13–27.11: 15 дней, до «cero horas del día inmediatamente anterior a la votación» ([LOREG](https://www.boe.es/buscar/act.php?id=BOE-A-1985-11672), ст. 51).
- **Опросы:** «Durante los cinco días anteriores al de la votación queda prohibida la publicación y difusión o reproducción de sondeos electorales por cualquier medio de comunicación» (ст. 69.7). Для 29.11 — с 24.11 до закрытия участков. Штраф — €3 000–30 000 (ст. 153.2). JEC применяла запрет к цифровому СМИ — дело Electomanía о «EmojiPanel» ([Qué!](https://www.que.es/2022/02/25/jec-expediente-electomania-sondeos-13f/)) `[вторичный]`.
- **Jornada de reflexión (28.11):** «No puede difundirse propaganda electoral ni realizarse acto alguno de campaña» после конца кампании (ст. 53). Агитация после окончания кампании — преступление (ст. 144.1.a). Нейтральная новость — не агитация, но 28.11 политических роликов о кандидатах не выпускаем.
- Касается любого канала, который пишет о выборах в Испании, в том числе американского `[толкование]`.

### <a id="es-ttpa"></a>5.6 Политическая реклама в ЕС (Regulation (EU) 2024/900, TTPA)
- Применяется с 10.10.2025. Редакционный контент политической рекламой не считается, «unless specific payment or other remuneration is provided … by third parties» (ст. 1(2)). Продвижение такого контента может стать рекламой (recital 29) ([ELI](http://data.europa.eu/eli/reg/2024/900/oj)). Это подтверждает запрет платного продвижения политики в policy.md; правила площадок — `platforms/youtube/rules.md`, `platforms/instagram/rules.md`.

## 6. США: что касается именно иностранного оператора

### <a id="us-fec-foreign-press"></a>6.1 Media exemption FEC для прессы под иностранным управлением
- Решения Комиссии о том, что иностранная собственность или контроль лишают press exemption, мы не нашли. Закон исключает только facilities, «owned or controlled by any political party, political committee, or candidate» (`countries/us/legal.md#election-federal`).
- MUR 6481 (RTTV America, 2014): Комиссия признала RTTV press entity, но вопрос о иностранце не решала — RTTV принадлежала гражданину США ([FEC](https://www.fec.gov/files/legal/murs/6481/14044354334.pdf)).
- MURs 7812/7825/7869 (AFP, 2021): Office of General Counsel — «notwithstanding AFP's status as a foreign national, it appears that the media exemption would likely apply» ([OGC](https://www.fec.gov/files/legal/murs/7869/7869_10.pdf)). Комиссия закрыла дело 6–0, но в итоговом анализе этой фразы нет ([F&LA](https://www.fec.gov/files/legal/murs/7812/7812_20.pdf)). Это позиция аппарата, а не Комиссии.
- Вывод: вопрос открыт — это первый вопрос медиаюристу. До ответа канал работает строго как пресса, без агитации и платного продвижения (`countries/us/legal.md#election-foreign`).

### <a id="us-fara"></a>6.2 FARA: оператор и подрядчики в США
- Оператор сам — **foreign principal**: «a person outside of the United States, unless … an individual and a citizen of and domiciled within the United States» ([22 U.S.C. §611(b)(2)](https://www.law.cornell.edu/uscode/text/22/611); [DOJ FAQ](https://www.justice.gov/nsd-fara/frequently-asked-questions)).
- Agent (§611(c)(1)) — тот, кто действует «at the order, request, or under the direction or control» foreign principal и «within the United States» ведёт political activities (§611(o)) или работает как «publicity agent» (§611(h) — распространение «information or matter of any kind»).
- **Оператор**, который делает собственный контент, — не agent: он ни на кого не работает `[толкование; позиции DOJ нет]`. Риск появится, если деньги или указания придут от иностранного правительства, партии или компании. DOJ толкует «within the United States» широко: онлайн-присутствие «is clearly viewable in the United States» ([AO 17.02.2023](https://www.justice.gov/nsd-fara/media/1290011/dl?inline)).
- **Подрядчики в США** (монтажёр, голос, американская LLC) работают по заданию foreign principal. Удалённая работа, которая «will originate with you in the United States», — это действия «within the United States» ([AO 14.02.2023](https://www.justice.gov/nsd-fara/media/1290006/dl?inline)).
  - Исключение для прессы §611(d) требует ≥ 80% собственности граждан США: «only if they are at least 80 percent owned by U.S. citizens» ([DOJ, CGTN](https://www.justice.gov/media/1070346/dl?inline)). LLC оператора под него не попадёт.
  - Исключение §613(d)(2) («not serving predominantly a foreign interest», [§613](https://www.law.cornell.edu/uscode/text/22/613)) DOJ применяет непоследовательно `[вторичный]`.
  - Зарегистрированный agent маркирует материалы по §614.
- Политика DOJ: уголовное преследование по FARA ограничено «conduct similar to more traditional espionage», FARA Unit переключён на civil enforcement ([AG memo, 05.02.2025](https://www.justice.gov/ag/media/1388541/dl)). 16.09.2026 DOJ напомнил, что нарушителям грозит «civil or criminal liability» ([DOJ](https://www.justice.gov/opa/pr/liability-unregistered-foreign-agents)).
  - NPRM от 02.01.2025 (маркировка онлайн-видео «reasonably calculated to reach an audience in the United States») финальным правилом не стал ([FR](https://www.govinfo.gov/content/pkg/FR-2025-01-02/pdf/2024-30871.pdf)).
- Для нас: подрядчиков в США на редакционную работу (сценарий, монтаж, голос, продвижение) — только после консультации FARA-юриста. Чисто технические услуги (облако, API) — вопрос к нему же `[не проверено]`.

### <a id="us-judgments"></a>6.3 Американское судебное решение в Испании (контекст риска)
- Договора США–Испания о признании решений нет. Hague Judgments Convention 2019 для ЕС действует с 01.09.2023; США её подписали, но не ратифицировали. К тому же она исключает «defamation» и «privacy» (ст. 2(1)(k), (l)) ([HCCH status](https://www.hcch.net/en/instruments/conventions/status-table/?cid=137); [текст](https://www.hcch.net/en/instruments/conventions/full-text/?cid=137)).
- Значит, исполнение возможно только через exequatur по Ley 29/2015 ([BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2015-8564)): суд первой инстанции по домицилию ответчика (ст. 52), пересмотра по существу нет (ст. 48).
  - Основания отказа (ст. 46.1): решение «contrarias al orden público»; «manifiesta infracción de los derechos de defensa» (в том числе заочное решение без надлежащего извещения); нет «conexión razonable» с судом.
  - Punitive damages Верховный суд Испании, по сообщениям, не считает противоречащими orden público per se (Auto TS 13.11.2001) `[вторичный, не проверено]`.
- Anti-SLAPP Directive (EU) 2024/1069: отказ признавать решение третьей страны по делу против лица из ЕС, если иск «manifiestamente infundada o abusiva» (ст. 16); иск о возмещении к истцу по месту своего домицилия (ст. 17) ([текст](https://www.boe.es/buscar/doc.php?id=DOUE-L-2024-80518)). Срок транспозиции — 07.05.2026. Испания одобрила проекты только в первом чтении (14.04.2026, [MPR](https://www.mpr.gob.es/prencom/notas/paginas/2026/directiva-anti-slapp.aspx)); закона нет.
- SPEECH Act ([28 U.S.C. §4102](https://www.law.cornell.edu/uscode/text/28/4102)) защищает ответчиков в США от иностранных решений о диффамации. Американское решение в Испании он не затрагивает.
- Вывод: решение суда США против оператора не исполняется в Испании автоматически, но может быть исполнено, если процесс был надлежащим. Кроме того, истец может подать иск сразу в Испании ([#scope](#scope)). Главная защита — точность и досье.

### <a id="us-tax-withholding"></a>6.4 Удержание налога США с выплат площадок
- YouTube: Google удерживает налог «when a YPP creator … earns royalty revenue from viewers in the U.S.». С налоговыми данными — 0–30% от дохода от зрителей из США; без них — «24% of total earnings worldwide» ([YouTube Help](https://support.google.com/youtube/answer/10391362)). Ставку по договору определяет налоговый инструмент AdSense ([Payments](https://support.google.com/paymentscenter/answer/10349995)).
- W-8BEN: для льгот по договору можно указать foreign TIN (испанский NIF) в строке 6a вместо US TIN ([IRS](https://www.irs.gov/instructions/iw8ben)).
- Договор США–Испания после Протокола 2013 года: роялти «shall be taxable only in that other State» ([Treasury](https://home.treasury.gov/system/files/131/Treaty-Spain-Protocol-1-14-2013.pdf)); в таблице IRS для copyright royalties Испании — **0%** ([IRS Table 1](https://www.irs.gov/pub/irs-lbi/tax-treaty-table-1.pdf)). Какую ставку Google фактически применяет — `[не проверено]`.
- TikTok и Meta: нерезиденты подают W-8BEN; без документов TikTok удерживает «typically … 30% on U.S. source income» ([TikTok](https://support.tiktok.com/en/business-and-creator/creator-and-business-accounts/tax-information-for-creators) `[ссылка не открыта]`; [Meta](https://www.facebook.com/business/help/1046259128769008) `[ссылка не открыта]`). Допускают ли CRP TikTok и монетизация Instagram оператора из Испании на американском канале — `[не проверено]` (`platforms/tiktok/rules.md`, `platforms/instagram/rules.md`: нужно жить в стране программы).
- В Испании налог, удержанный за рубежом, засчитывается в пределах средней ставки (ст. 80 LIRPF). При ставке 0% по договору лишнее удержание, скорее всего, придётся возвращать через IRS, а не засчитывать `[не проверено]`.

## <a id="es-tax"></a>7. Испания: налоги и регистрация — чек-лист для gestor
- **Вид дохода.** Доход от монетизации — rendimientos de actividades económicas (ст. 27 LIRPF) ([DGT V0992-16](https://petete.tributos.hacienda.gob.es/consultas/?num_consulta=V0992-16)). Эпиграф IAE в разъяснениях DGT разный: 961.1 + 844 ([V1895-24](https://petete.tributos.hacienda.gob.es/consultas/?num_consulta=V1895-24)) или 869, sección 2ª ([V2390-24](https://petete.tributos.hacienda.gob.es/consultas/?num_consulta=V2390-24)). Выбрать с gestor: от этого зависят удержания и modelo 130.
- **Регистрация в Hacienda — только modelo 036.** Modelo 037 упразднён с 03.02.2025 (Orden HAC/1526/2024, [AEAT](https://sede.agenciatributaria.gob.es/Sede/en_gb/ayuda/manuales-videos-folletos/manuales-practicos/manual-iva-2025/capitulo-01-novedades-destacar-2025/modelo-037.html)). Физлица освобождены от IAE (ст. 82.1.c TRLRHL), но эпиграф указывается в 036 ([AEAT](https://sede.agenciatributaria.gob.es/Sede/censos-nif-domicilio-fiscal/tramites-censales-relacionados-empresarios-profesionales-retenedores/impuesto-sobre-actividades-economicas.html)).
- **RETA** — при деятельности «de forma habitual, personal, directa» (ст. 305.1 LGSS, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2015-11724)). Льготная cuota для новых autónomos — 12 месяцев плюс ещё 12 при доходе ниже SMI (ст. 38 ter Ley 20/2007, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2007-13409)); её размер на 2026 год `[не проверено]`. Шкалы взносов по реальному доходу на 2026 год — Orden PJC/297/2026, ст. 18 ([BOE](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2026-7296)).
- **IVA.**
  - YouTube в ЕЭЗ — Google Ireland Limited ([ToS](https://www.youtube.com/t/terms)); AdSense платит под reverse charge ([AdSense](https://support.google.com/adsense/answer/1322031)). Такие услуги «no sujetos» в Испании (ст. 69 LIVA), но это prestación intracomunitaria: нужны регистрация в ROI, modelo 349 и пометка «inversión del sujeto pasivo» в счёте ([V2594-21](https://petete.tributos.hacienda.gob.es/consultas/?num_consulta=V2594-21)). Плюс modelo 303 и 390.
  - TikTok: выплаты Rewards в ЕЭЗ ведёт TikTok Information Technologies UK Limited ([Rewards Policy](https://www.tiktok.com/legal/page/global/rewards-policy-eea/en)) — компания не из ЕС, поэтому 349 может не понадобиться `[толкование]`.
  - Meta: какое юрлицо платит, условия не фиксируют ([terms](https://www.facebook.com/legal/content_monetization_terms)).
- **IRPF.** Резидент платит налог со всемирного дохода (ст. 2, 9 LIRPF, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2006-20764)). Иностранные площадки IRPF не удерживают ([V3103-23](https://petete.tributos.hacienda.gob.es/consultas/?num_consulta=V3103-23)), поэтому modelo 130 — ежеквартально ([AEAT](https://sede.agenciatributaria.gob.es/Sede/irpf/retenciones-ingresos-cuenta-pagos-fraccionados/pagos-fraccionados.html)); годовая декларация — modelo 100; зачёт налога США — ст. 80.
- **Иностранные счета.** Modelo 720 — если активы за рубежом больше €50 000; применим ли он к балансам AdSense и Payoneer — `[не проверено]`. Modelo 721 — только для криптовалюты.
- **Счета-фактуры.** Meta выставляет счета сама по «acuerdo de autofacturación» ([terms](https://www.facebook.com/legal/content_monetization_terms)); Google — Payment Receipts ([AdSense](https://support.google.com/adsense/answer/7164701)). Verifactu для autónomos — «antes del 1 de julio de 2027» (RD 1007/2023, [BOE](https://www.boe.es/buscar/act.php?id=BOE-A-2023-24840)). Сообщают ли площадки доходы авторов по DAC7 (modelo 238) — `[не проверено]`.

## <a id="red-flags"></a>8. Красные флаги для ревьюера

Серьёзность — по шкале Legal (`newsroom/teams/legal/_common.md`). Флаги права США — в `countries/us/legal.md#red-flags`.

| Элемент ролика или работы | Правовой риск: кто и что может предъявить | Как исправить |
|---|---|---|
| Нет метки «AI» в начале ролика, AI-голос не назван, нет флага площадки | **high** · AESIA: ст. 50(4), до €15 млн (1.1, 1.5) — оба канала | метка «AI» в начале и плашка (1.4), флаги площадок |
| Реалистичный образ или голос реального человека, «речь» политика | **blocker** · AESIA: deep fake (1.2); человек: LO 1/1982, ст. 7.6–7.7 (5.1) | только явный pixel-art и задокументированные цитаты через диктора |
| Король или королевская семья в унизительной или выдуманной сцене | **blocker** · прокуратура: CP, ст. 490.3, 491.2 (5.3) | нейтральная сцена: дворец, документ; дословная цитата |
| Обвинение названного человека в преступлении без официального документа | **blocker** · человек: calumnia (ст. 205–206), LO 1/1982, ст. 7.7 — иск в Испании по любому каналу (0, 5.1, 5.3) | формулировки `countries/us/legal.md#criminal-terms`; источник — документ из досье |
| Оценка с фактическим ядром от лица канала («ladrón», «corrupto») | **high** · человек: injuria (ст. 208–209), LO 1/1982, ст. 7.7 | атрибутировать мнение конкретному лицу |
| Утверждение со статусом `unverified` в досье подано как факт | **high** · нет «información veraz» (STC 6/1988) — защиты нет (5.1) | понизить до «по данным X», дать позицию стороны |
| Имя, лицо или узнаваемые детали несовершеннолетнего, особенно в уголовном контексте | **blocker** · Ministerio Fiscal: LO 1/1996, ст. 4; LO 5/2000, ст. 35.2; LGCA, ст. 95.2 (5.4) | не называть и не изображать |
| Голос, имя или узнаваемые детали жертвы сексуального насилия, даже из материалов суда | **blocker** · AEPD: до €50 000 по прецеденту (4.8) | только обезличенный пересказ |
| Кадр или данные частного лица из вирусного видео | **high** · AEPD: ст. 5(1)(c) GDPR (4.8) | не использовать; обезличенная схема |
| Опрос о выборах в Испании 24–29.11.2026 (любой канал) | **blocker** · JEC: ст. 69.7 LOREG, €3 000–30 000 (5.5) | не публиковать даже как «утечку» или игру |
| Политический ролик о кандидатах Испании 28.11.2026 | **high** · JEC, прокуратура: ст. 53, 144.1.a (5.5) | перенести на 30.11 |
| Восхваление теракта или вооружённой группы, ненависть по политическим взглядам | **blocker** · прокуратура: CP, ст. 578, 510; LGCA, ст. 4.2 (5.3, 3.2) | новостная рамка, атрибуция, без оценок |
| Платное продвижение или оплаченный третьими лицами политический ролик | **blocker** · TTPA (5.6); FEC (`countries/us/legal.md#election-foreign`) | не продвигать; оплату не брать |
| Спонсорская интеграция без «publicidad»/«publi» в кадре | **high** · CNMC: LGCA, ст. 121–134, после порога (3.2–3.3) | метка в кадре и голосом плюс инструмент площадки |
| Требование rectificación от человека или СМИ в Испании | **blocker** · суд: LO 2/1984, публикация за 3 дня, иск за 7 дней (5.2) | порядок 5.2 и `countries/us/legal.md#defamation-retraction`: копии и юрист в тот же день |
| Запрос на удаление, возражение или обновление (ст. 17, 21 GDPR; ст. 86 LOPDGDD) | **high** · AEPD: жалоба; срок ответа — месяц (4.4) | модератору и юристу; aviso de actualización или мотивированный отказ |
| В досье лишние данные частных лиц или уголовные данные без публичного источника | **high** · AEPD: ст. 5(1)(c), 10 GDPR; ст. 10 LOPDGDD (4.3) | убрать, оставить ссылку на публикацию |
| Страница источников с аналитикой или cookies третьих сторон, без aviso legal | **medium** · AEPD и министерство: LSSI, ст. 22.2, 10 (4.6) | без cookies, кроме необходимых; aviso legal |
| Задача, деньги или «темы» от иностранного правительства, партии или компании | **blocker** · DOJ: FARA (6.2) | отказаться, эскалировать владельцу |
| Подрядчик в США делает сценарий, монтаж, голос или продвижение | **high** · DOJ: FARA, publicity agent (6.2) | не нанимать без FARA-юриста |

## <a id="owner-tasks"></a>9. Дела владельца (не проверяются по ролику, делаются один раз)

**До запуска канала для США**
- Gestor: эпиграф IAE, modelo 036, alta en RETA (до начала деятельности), ROI и 349 для Google Ireland, 130/303 по кварталам; самофактурирование площадок и Verifactu к 01.07.2027 ([#es-tax](#es-tax)).
- AdSense: W-8BEN с NIF и ставкой по договору (0% copyright royalties); налоговые формы TikTok и Meta ([#us-tax-withholding](#us-tax-withholding)). Проверить, доступны ли CRP TikTok и монетизация Instagram оператору из Испании на американском канале.
- Медиаюрист США: media exemption для прессы под иностранным управлением ([#us-fec-foreign-press](#us-fec-foreign-press)); FARA для будущих подрядчиков ([#us-fara](#us-fara)); разовая проверка `countries/us/legal.md`.
- AI Act: схема маркировки по Code of Practice (метка «AI» в начале, «AI voice»); политика editorial responsibility с опубликованным контактом; чек-лист и журнал проверки у модератора ([#ai-act-our-case](#ai-act-our-case)).
- GDPR: реестр обработки; оценка legitimate interest для досье; уведомление о приватности на сайте источников; договоры processor; хранение в ЕС или DPF/SCC; порядок ответа на запросы субъектов за месяц ([#gdpr-records](#gdpr-records), [#gdpr-rights](#gdpr-rights)).
- Срок хранения досье: 2 или 4 года — решить с юристом в Испании ([#gdpr-rights](#gdpr-rights)).
- LSSI: aviso legal на домене источников — чей адрес указывать (домицилий или постоянное заведение); без лишних cookies ([#lssi](#lssi)).
- hub: счётчики порогов LGCA по оператору и сигнал при 70% ([#lgca-watch](#lgca-watch)).
- Спросить юриста о страховании ответственности за публикации — до первых претензий.

**До запуска канала для Испании**
- Создать `countries/es/legal.md` и `countries/es/policy.md`: LO 1/1982, LO 2/1984, CP, LOREG, практика AEPD; проверить их у испанского медиаюриста.
- Порядок rectificación с SLA 3 дня и опубликованный контакт для опровержений ([#es-rectification](#es-rectification)).
- Не запускать канал в кампанию 13–29.11.2026 без правил на дни запрета опросов и jornada de reflexión ([#es-elections](#es-elections)). Лучше — после 29.11.
- Метки возраста по ст. 99.1 LGCA и регистрация в Registro Estatal — к моменту, когда подойдём к порогам ([#lgca-duties](#lgca-duties)).

**Следить после выборов 29.11.2026 и созыва новых Cortes**
- Законопроекты, которые caducaron 06.10.2026 и могут вернуться: закон об AI (санкции за маркировку), реформа LO 1/1982 (deepfakes, caricatura), новая LO о rectificación (20 дней), LO о защите несовершеннолетних в цифровой среде, отмена injurias a la Corona. А также транспозиция anti-SLAPP, формальное принятие Guidelines по ст. 50 и апелляция по DPF (C-703/25 P).
