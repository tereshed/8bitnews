# ES — правовая база для Legal (8BitNewsES)

verified_at: 2026-11-04

> **Это не юридическая консультация.** Это рабочая база для агентов Legal, Research, Script, Video, QA и для модератора. Один раз её нужно проверить у испанского медиаюриста и перепроверять при смене формата. Cortes распущены 06.10.2026 и соберутся 23.12.2026 ([RD 806/2026](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2026-20742), ст. 1, 5), поэтому статусы законопроектов даны на дату `verified_at`.
> Файл дополняет [`policy.md`](policy.md) и [`operator/legal.md`](../../operator/legal.md). Там сказано, что канал делает, и лежит общее право оператора; здесь — как право применяется к сценарию, кадру и обложке одного ролика. Если правила расходятся, действует более строгое.
> Пометки: `[не проверено]` — не подтверждено первичным источником; `[вторичный]` — источник: СМИ, НКО, обзор; `[ссылка не открыта]` — не открылась роботу; `[толкование]` — наш вывод из текста, позиции органа или суда нет.
> Как сверяли (04.11.2026): статьи BOE — через открытый API BOE (`datosabiertos`), ссылки `act.php` ведут на тот же консолидированный текст; решения TC — по hj.tribunalconstitucional.es и BOE; доктрина JEC — по базе juntaelectoralcentral.es; акты и решения ЕС — по файлам Publications Office (cellar), EUR-Lex закрыт от роботов; ЕСПЧ — через конвертер HUDOC. Сайт Верховного суда (CENDOJ) мы не открывали: решения TS здесь известны только из цитат TC и JEC или из вторичных источников.
> Ссылка на раздел: `countries/es/legal.md#<id>`.

## <a id="scope"></a>0. Что здесь и что в `operator/legal.md`

| Вопрос | Где |
|---|---|
| AI Act, ст. 50: кто deployer, что такое deep fake, штрафы | `operator/legal.md#ai-act-art50`, `#ai-act-our-case`, `#ai-act-fines` |
| Пороги и обязанности «usuario de especial relevancia» | `operator/legal.md#lgca-thresholds`, `#lgca-duties` |
| GDPR, досье, запросы субъектов, хранение | `operator/legal.md#gdpr-role`, `#gdpr-special`, `#gdpr-rights` |
| LO 1/1982: перечень вмешательств, презумпция вреда, срок 4 года | `operator/legal.md#es-honor` |
| Опровержение за 3 дня (LO 2/1984) | `operator/legal.md#es-rectification` |
| Calumnia, injuria, составы и наказания | `operator/legal.md#es-criminal` |
| Законопроекты, которые могут вернуться | `operator/legal.md#owner-tasks` |

- Отвечает оператор лично. JEC тоже наказывает конкретного человека: директора газеты за интервью в день размышления ([Acuerdo 25/2018](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=64812&template=Doctrina/JEC_Detalle)), единственного администратора компании, которой принадлежит блог ([Acuerdo 93/2022](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=80423&template=Doctrina/JEC_Detalle)).
- Кто и что может предъявить: герой сюжета — иск о honor, intimidad, imagen; querella о calumnia или injuria; rectificación. Ministerio Fiscal — составы о Короне, символах, ненависти, терроризме, несовершеннолетних. JEC и juntas — опросы и агитация. AEPD — персональные данные. AESIA — маркировка AI. CNMC — реклама после порогов LGCA. Правообладатель — иск, уведомление на площадке.

## 1. Правдивость, мнение и чужие слова

### <a id="veracity"></a>1.1 Veracidad и стандарт проверки
- [CE, ст. 20.1.d](https://www.boe.es/buscar/act.php?id=BOE-A-1978-31229) защищает право сообщать «información veraz». Veraz не значит абсолютно точная. Защиты лишены те, кто передаёт как факты «simples rumores carentes de toda constatación» или «meras invenciones o insinuaciones». Защищена информация, если «se ha observado el deber de comprobar su veracidad mediante las oportunas averiguaciones propias de un profesional diligente» ([STC 62/2025, FJ 2](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2025-7428)).
- **Досье — это и есть доказательство.** «[E]l objeto de su prueba no serán los hechos en sí objeto de narración, sino los hechos, datos o fuentes de información empleados» (там же).
- Требование зависит от содержания ролика (там же, FJ 2):
  - сведения порочат человека — «especial intensidad»;
  - приписывается преступление — «en su máxima intensidad» (FJ 4.c);
  - «debe ponderarse el respeto a la presunción de inocencia»;
  - учитываются характер новости, источник и «las posibilidades efectivas de contrastarla».
- **Свой текст или чужие слова.** Если информация «asumida por el medio y su autor como propia», долг проверки «no admite atenuación o flexibilidad alguna» ([STC 76/2002, FJ 3](https://hj.tribunalconstitucional.es/es/Resolucion/Show/4612)).
- **Надёжный источник.** Если у него «características objetivas que la hacen fidedigna, seria o fiable», может хватить проверки «la exactitud o identidad de la fuente», особенно если он назван в материале ([STC 158/2003, FJ 5](https://hj.tribunalconstitucional.es/es/Resolucion/Show/4933), цит. в STC 62/2025). Пресс-релиз Guardia Civil: нельзя требовать контраста с другими источниками, «sino solo la seguridad de que la fuente era el órgano que se dice» ([STC 178/1993, FJ 5](https://hj.tribunalconstitucional.es/es/Resolucion/Show/2307)). Это близкий аналог американского fair report, но он держится на близости органа к расследованию.
- **Видео строже текста.** Ответственность «es si cabe mayor cuando se trata de medios audiovisuales», потому что «por las imágenes los medios audiovisuales pueden transmitir mensajes que un medio escrito no es apto para trasmitir» ([STC 139/2007, FJ 11](https://hj.tribunalconstitucional.es/es/Resolucion/Show/6106), со ссылкой на Jersild).
- Для нас: стандарт — «profesional diligente». Соответствует ли ему фактчек силами LLM, решения мы не нашли `[не проверено]`. С 09.11.2026 человека перед публикацией нет (решение владельца), поэтому сюжеты, суть которых — обвинение названному человеку, не берём (`policy.md`). Обвинение, оставшееся мимоходом, сверяют с первичным документом `fact_checker` и Legal, и след проверки остаётся в досье; без такого документа это `blocker`. Хватает ли этого для «profesional diligente», не проверено; риск на операторе (см. также `operator/legal.md#ai-act-our-case`).

### <a id="opinion-fact"></a>1.2 Мнение и факт
- Факт и оценка редко разделены: «la comunicación de hechos o de noticias no se da nunca en un estado químicamente puro». Смотрят на элемент, который «aparece como preponderante» ([STC 6/1988, FJ 5](https://hj.tribunalconstitucional.es/es/Resolucion/Show/947)).
- Для мнения предела правдивости нет: оно не поддаётся проверке на точность, и в нём возможна критика, даже «desabrida» ([STC 23/2010, FJ 3](https://hj.tribunalconstitucional.es/es/Resolucion/Show/6662)). Предел — оскорбление: JEC, пересказывая практику TC, называет законной критику «hiriente, molesta o desabrida», а предел видит в «insulto o la utilización de expresiones vejatorias» ([Acuerdo 197/2024](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=132281&template=Doctrina/JEC_Detalle)).
- Подозрение, выданное за мнение, защиты не получает. В STC 139/2007 родственница на ТВ заявила, что муж пропавшей её убил и «es un asesino»: она «sustituyó los datos existentes por sus personales y sesgados criterios» (FJ 10); TC защитил honor мужа и дочерей.
- Для нас: канал оценок не даёт (`policy.md`). Приписывание мнения не спасает его фактическое ядро: «según Y, X robó» проверяем как «X robó», если не выполнены условия [1.3](#reportaje-neutral).

### <a id="reportaje-neutral"></a>1.3 Reportaje neutral
Главная доктрина для канала, который передаёт чужие утверждения, и она узка. В отличие от США ([`countries/us/legal.md#defamation-republication`](../us/legal.md#defamation-republication)) в Испании такая защита признана, но почти никогда не покрывает наш ролик целиком.

**Условия** ([STC 76/2002, FJ 4](https://hj.tribunalconstitucional.es/es/Resolucion/Show/4612); [STC 54/2004, FJ 7](https://hj.tribunalconstitucional.es/es/Resolucion/Show/5059); [STC 62/2025, FJ 2](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2025-7428)):
1. Новость — сами заявления, которые сами по себе «noticia» и вложены «en boca de personas determinadas». Анонимные источники исключают доктрину: «[n]o hay reportaje neutral cuando no se determina quién hizo las declaraciones».
2. Медиа — «mero transmisor»: излагает «sin alterar su importancia en el conjunto de la noticia». Защиты нет, «cuando se reelabora la noticia, ni cuando el medio provoca la noticia»; можно лишь воспроизводить то, что «ya sea de algún modo conocido».
3. Тогда правдивость сводится к «la veracidad objetiva de la existencia de la declaración», и медиа не отвечает за содержание. Чем дальше от этой модели, тем больше ответственность: «la mayor o menor proximidad al reportaje neutral propio modula la responsabilidad».

**Что доказываем:** заявление существует и «coincide fielmente» с переданным; его автор установлен; нет «indicios racionales de falsedad evidente» (STC 62/2025, FJ 2, по STC 134/1999, FJ 4).

**Что ломает защиту:**
- **Монтаж.** Медиа манипулирует «mediante su artero fraccionamiento», вмешивается «con manifestaciones propias» или строит материал «con textos o imágenes» так, что информация становится его собственной ([STC 134/1999, FJ 4](https://hj.tribunalconstitucional.es/es/Resolucion/Show/3876); STC 139/2007, FJ 11). Важно, как материал подан публике: в STC 139/2007 слово давали родственницам, но программа «dista del concepto de reportaje neutral propio» (FJ 12).
- **Заголовок.** Защита не распространяется на заголовки, которые под видом нейтральности «están destinados a sembrar en el gran público dudas sobre la honorabilidad de las personas aludidas». Заголовок-вопрос с обвинением медиа задаёт от себя и этим ставит нейтральность под вопрос; в том деле газету спасли ясные ссылки на источник тут же и анализ материала в целом (STC 54/2004, FJ 8). Хук и обложку читают отдельно от ролика.
- **Агентская лента.** Новость агентства под доктрину не подпадает. «[N]i la doctrina del reportaje neutral puede suponer una coartada para la vulneración de derechos fundamentales, ni la publicación de noticias procedentes de agencias de información libera al medio de su deber de diligencia, aunque lo pueda moderar». Смягчение действует, только если новость выходит «sin modificar ni asumir como propia» со ссылкой на агентство (STC 62/2025, FJ 3, Пленум; то же в [STC 87/2025](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2025-9631) и [STC 101/2025](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2025-11315)).
- **Картинка.** См. [2.2](#likeness-scenes).

**Для нас `[толкование]`:**
- Ролик — «собственный анализ из нескольких источников» (`policy.md`), то есть reelaboración. Ролик в целом reportaje neutral не является: на него действует полный долг проверки из [1.1](#veracity). Смягчение для агентских новостей к нам тоже не подходит: мы их переписываем.
- Защита возможна для отдельной дословной цитаты названного лица, если само заявление — новость, а её окружение нейтрально. Условия: автор назван; текст совпадает с первоисточником; ссылка на него в досье; нет явных признаков ложности; есть позиция обвиняемой стороны или пометка, что она не ответила.
- Обвинение от анонимного источника, пересказанное со ссылкой на СМИ («según El País, fuentes de la investigación…»), reportaje neutral не даёт. Это наша собственная информация: нужен документ или несколько независимых подтверждений.
- В хук, заголовок и на обложку чужое обвинение не выносим, вопросов-намёков не ставим.

### <a id="public-private"></a>1.4 Публичные и частные лица
- Защита максимальна, когда речь о вопросах публичного значения и о лицах, «obligadas por ello a soportar un cierto riesgo» из-за публичной роли ([STC 107/1988, FJ 2](https://hj.tribunalconstitucional.es/es/Resolucion/Show/1048)). Это ослабляет honor, но не отменяет долг проверки: правдивость обязательна и о политиках.
- Уголовная хроника может быть новостью «con independencia del carácter de sujeto privado de la persona afectada» (STC 62/2025, FJ 2). Однако показывать такого человека опаснее: неизвестный публике, «la publicación de su fotografía puede considerarse una injerencia más importante que un reportaje escrito» (там же, FJ 4.c, по Von Hannover).
- Интерес — это «relevancia comunitaria, y no la simple satisfacción de la curiosidad ajena» ([STC 185/2002, FJ 4](https://hj.tribunalconstitucional.es/es/Resolucion/Show/4721)).
- Для нас: сообщать о событии с частным лицом можно, называть и рисовать его — нет (`policy.md`). Он ничего не теряет от того, что событие попало в новости.

## 2. Образ и голос

### <a id="likeness-caricature"></a>2.1 Caricatura и pixel-art реальных людей
- Базовые нормы — в `operator/legal.md#es-honor`. Ст. 7.5 [LO 1/1982](https://www.boe.es/buscar/act.php?id=BOE-A-1982-11196) говорит об изображении «por fotografía, filme, o cualquier otro procedimiento», поэтому узнаваемый спрайт — тоже образ человека `[толкование]`.
- Ст. 8.2.b разрешает «la caricatura de dichas personas, de acuerdo con el uso social». «Dichas personas» — те, кто занимает «cargo público» или имеет «profesión de notoriedad o proyección pública» (8.2.a). **На частных лиц норма не распространяется.** Не распространяется она и на тех, чьи функции «necesiten el anonimato» (8.2, последний абзац): агенты, сотрудники спецслужб `[толкование]`.
- Определение TC: карикатура — «toda creación satírica realizada a partir de las facciones y el aspecto de alguien, deformando su realidad» ([STC 23/2010, FJ 5](https://hj.tribunalconstitucional.es/es/Resolucion/Show/6662)). Она ценна как способ выразить «críticas sociales o políticas». Юмористический тон сам по себе ничего не решает: определяет «un interés democrático superior», а карикатура, созданная ради денег или чтобы унизить, защиты не получает. В деле фотомонтажа в юмористическом журнале в ампаро отказано.
- TS в том же деле (07.03.2006): частота такого использования не делает его «uso social» — «socialmente inadecuado por repetido que sea» ([Economist & Jurist](https://www.economistjurist.es/actualidad-juridica/al-dia/intromision-ilegitima-en-el-derecho-a-la-propia-imagen/)) `[вторичный]`.
- **Для нас `[толкование]`:**
  - Наш спрайт — не сатира: новости и сатиру не смешиваем. Поэтому ст. 8.2.b — лишь дополнительный довод. Главное основание — ст. 20.1.d CE и связь образа с новостью о публичной роли человека.
  - Спрайт политика или другого публичного лица в новости о нём допустим. Спрайт частного лица недопустим и силуэтом с узнаваемыми чертами тоже.
  - Шутливый или игровой образ реального человека не защищён ссылкой на жанр. Реальные люди — не боссы, не враги, не «проигравшие» (`format.md`).
  - Образ реального человека вне новости о нём (реклама, мерч, продвижение канала) теряет защиту ([2.3](#likeness-voice)).

### <a id="likeness-scenes"></a>2.2 Придуманные сцены и связь образа с новостью
- **STC 62/2025 — ключевое решение для видео.** Текст был верным, но в ролике показали боксёра, не имевшего отношения к убийству. TC подтвердил нарушение honor и imagen: при совместном показе текста и изображения «aunque estos fueran ciertos con carácter general, la información se convertía en inveraz al asociarse con la imagen» (FJ 4.b). Краткость кадра не спасла: образ «no accesoria». Ссылка на профессиональное агентство тоже.
- Изображение «carecía de cualquier interés en relación con la noticia» — и этого достаточно для вмешательства (FJ 4.a).
- Directive (EU) 2016/343, recital 20: власти не должны показывать подозреваемых виновными с помощью «medios de coerción física como esposas» ([текст](http://data.europa.eu/eli/dir/2016/343/oj)). Адресована властям, но логика та же, что в правиле policy.md о наручниках.
- Для нас:
  - реальный человек в кадре только в сцене, которая есть в досье. Ни ареста, ни решётки, ни места преступления, ни встречи, которой не было;
  - спрайт сверяем с именем: путаница тёзок, супругов, коллег делает верный текст ложным;
  - обобщённый «типичный» персонаж в истории о преступлении не должен походить на реального человека;
  - сексуализированных образов реальных людей не бывает ни в каком стиле (STC 23/2010; `countries/us/legal.md#likeness-intimate`);
  - «речь» политика в кадре ближе к deep fake: `operator/legal.md#ai-act-our-case`.

### <a id="likeness-voice"></a>2.3 Голос (ст. 7.6)
- Вмешательство — «[l]a utilización del nombre, de la voz o de la imagen de una persona para fines publicitarios, comerciales o de naturaleza análoga». Голос назван прямо; требования клонирования в норме нет `[толкование]`.
- Считать ли монетизацию коммерческой целью, неясно. В деле «El Informal» (сатирический дубляж голоса полицейского) Audiencia Provincial сочла программу коммерческой, а TS 17.02.2009 нарушения не нашёл из-за сатирического контекста ([Legal Today](https://www.legaltoday.com/historico/jurisprudencia/jurisprudencia-civil/sentencia-del-tribunal-supremo-sala-de-lo-civil-de-17-febrero-2009-2009-03-11/)) `[вторичный; мотивировка прочитана не целиком]`. Нашей новостной подаче этот довод не помогает.
- Решения именно о клоне голоса мы не нашли `[не проверено]`. Законопроект 121/000103 со специальной нормой о сгенерированных голосах caducado (`operator/legal.md#es-honor`).
- Для нас: голос ведущего — лицензированный синтетический, не похожий на реального человека. Реальные голоса не клонируем и не имитируем, даже для подлинной цитаты: читает ведущий с атрибуцией, текст в кадре.

## 3. Приватность, несовершеннолетние, жертвы

### <a id="privacy"></a>3.1 Приватность
- Ст. 7.3 LO 1/1982 и практика AEPD — в `operator/legal.md#es-honor`, `#gdpr-fines`. Правдивость здесь не защищает: в STC 185/2002 (FJ 4) сведения были верны, но «fueron desvelados de forma innecesaria».
- Здоровье, сексуальная жизнь, адрес, семья частных лиц — только если это суть события. Этого почти никогда нет.

### <a id="minors"></a>3.2 Несовершеннолетние
- Основные нормы — `operator/legal.md#es-minors`. В дополнение:
  - [LECrim, ст. 681.3](https://www.boe.es/buscar/act.php?id=BOE-A-1882-6036) запрещает «en todo caso» раскрывать личность жертв-несовершеннолетних и данные, которые позволяют опознать их «de forma directa o indirecta», а также получать и публиковать «imágenes suyas o de sus familiares»;
  - такой запрет касается и родителей: образ и имя отца выдают ребёнка. В [STC 127/2003](https://hj.tribunalconstitucional.es/es/Resolucion/Show/4902) статьи давали жертву-дочь инициалами, а TC признал нарушение её intimidad.
- Для нас: детей не называем и не рисуем (`policy.md`). Убираем школу, улицу, возраст, родственников и всё, что вместе указывает на ребёнка.

### <a id="victims"></a>3.3 Жертвы
- **Сексуальное насилие.** С 22.08.2024 тот же запрет ст. 681.3 LECrim распространяется на «las víctimas de los delitos de violencia sexual» по ст. 3 LO 10/2022. Американской защиты «правдиво и из материалов суда» здесь нет: нужно судить по этому запрету. Какая санкция ждёт СМИ именно за ст. 681.3, мы не установили `[не проверено]`. Практику штрафов AEPD (€50 000 за голос жертвы) см. в `operator/legal.md#gdpr-fines`.
- TC: возраст, имя, инициалы и номер дома «permitieron perfectamente a sus vecinos, allegados y conocidos la plena identificación de la víctima», а сведения были «irrelevante» для новости (STC 185/2002, FJ 4).
- **Любая жертва.** Суд может запретить публиковать личность и изображения жертвы и её семьи (ст. 681.2), ограничить съёмку и раскрытие личности жертв, свидетелей и экспертов (ст. 682). Такие запреты проверяем в материалах дела. [Ley 4/2015, ст. 34](https://www.boe.es/buscar/act.php?id=BOE-A-2015-4606): права жертв на «la intimidad, la imagen, la dignidad» «deberán ser respetados por los medios de comunicación social». Ст. 22 обязывает участников процесса защищать intimidad всех жертв.
- **Гендерное насилие.** [LO 1/2004, ст. 14](https://www.boe.es/buscar/act.php?id=BOE-A-2004-21760): информация должна защищать достоинство жертв и их детей; «se tendrá especial cuidado en el tratamiento gráfico de las informaciones». Ст. 63.1: данные жертв и детей защищены.
- **Терроризм.** [Ley 29/2011, ст. 61](https://www.boe.es/buscar/act.php?id=BOE-A-2011-15039): государство защищает достоинство жертв, власти предотвращают публичные действия, которые влекут их унижение. Сами жертвы могут защищать honor в суде. Уголовный состав — [5.5](#speech-terrorism).
- Для нас: жертву сексуального и гендерного насилия не рисуем и не называем, родственников тоже. Спрайт жертвы без её ясного и публичного участия не используем. Обезличенный рассказ и серьёзный режим (`format.md`).

## 4. Уголовные дела

### <a id="criminal-terms"></a>4.1 Стадии процесса и формулировки
Формулировка должна совпадать со стадией в досье. Нормы — [LECrim](https://www.boe.es/buscar/act.php?id=BOE-A-1882-6036). Примеры — ориентир для Script; дату и орган берём из источника.

| Стадия | Норма | Как говорим (ES) | Чего нельзя |
|---|---|---|---|
| Подано заявление, суд не решил | — | «X ha sido denunciado por Y»; «Y ha presentado una querella contra X; el juzgado aún no ha decidido si la admite» | «investigado», «acusado» |
| Проверка полиции или прокуратуры без суда | — | «la Fiscalía ha abierto diligencias»; «la Policía investiga los hechos» | «X está investigado» в процессуальном смысле, если этого нет в документе |
| Задержание | ст. 520.1 | «X ha sido detenido por la Policía, que le atribuye…» | «autor», «investigado» до решения суда |
| Заключение под стражу | ст. 520.1 | «el juez ha decretado prisión provisional para X, investigado por…» | «preso por matar»: мера не приговор |
| Investigado | ст. 118; LO 13/2015 | «X está investigado por un presunto delito de…»; «el juzgado investiga a X» | **«imputado»** (см. ниже), «culpable» |
| Encausado (procedimiento abreviado) | ст. 779.1.4ª; LO 13/2015 | «el juez ha acordado seguir la causa contra X por procedimiento abreviado, por…» | «condenado», «ya juzgado» |
| Procesado (sumario) | ст. 384 | «X ha sido procesado por…» | применять к procedimiento abreviado (ст. 757: до 9 лет) |
| Обвинение | ст. 650, 783 | «la Fiscalía acusa a X de… y pide N años»; «X, acusado de matar a Y» | «el asesino X», а также «el presunto asesino X» |
| Суд идёт | ст. 783 | «X, acusado de…, niega los hechos» | вывод до приговора |
| Conformidad | ст. 779.1.5ª | «X ha reconocido los hechos y aceptado la pena» | «confesó» без документа |
| Приговор не вступил в силу | ст. 141 | «condenado a N años en una sentencia que no es firme; cabe recurso» | «culpable» без оговорки; опускать «no es firme» |
| Приговор окончательный | ст. 141 | «condenado en sentencia firme» | — |
| Оправдание | — | «absuelto»; если сторона обжалует — сказать об этом | «absuelto por falta de pruebas», если суд так не сказал; намёки |
| Прекращение условное | ст. 641, 779.1.1ª | «el juzgado ha archivado provisionalmente la causa» | «declarado inocente», «caso cerrado» |
| Прекращение окончательное | ст. 637 | «sobreseimiento libre: el juzgado concluye que…» с основанием из решения | писать «archivo», если причину можно назвать точнее |
| Гражданский иск | — | «demandado»; «condenado a indemnizar por…» | «culpable», «delito» |
| Административное дело | — | «expedientado»; «sancionado con…» | «delito» |

- **«Imputado» не используем.** [LO 13/2015, преамбула V](https://www.boe.es/buscar/act.php?id=BOE-A-2015-10725), заменила его из-за «connotaciones negativas y estigmatizadoras»: investigado — «persona sometida a investigación», encausado — тот, кому суд после следствия «imputa formalmente» деяние. СМИ и источники слово всё ещё используют: в ролике переводим его на текущую стадию.
- **Sentencia firme** — «cuando no quepa contra ellas recurso alguno ordinario ni extraordinario» (ст. 141). Пока срок на обжалование идёт или жалоба не решена, говорим «no es firme».
- **Sobreseimiento provisional** — не решение о невиновности: «no resulte debidamente justificada la perpetración del delito» или нет оснований обвинять конкретного человека (ст. 641). Обратного вывода («всё-таки виновен, но не доказали») тоже не делаем.
- **«Presunto» — не волшебное слово** `[толкование]`: «el presunto asesino X» всё равно называет его убийцей. Лучше назвать стадию и документ.
- **Презумпция.** [CE, ст. 24.2](https://www.boe.es/buscar/act.php?id=BOE-A-1978-31229). [Directive (EU) 2016/343, ст. 4](http://data.europa.eu/eli/dir/2016/343/oj): «declaraciones públicas efectuadas por las autoridades públicas» не должны называть человека виновным; норма адресована властям и «se entiende sin perjuicio» национальной свободы прессы (recital 19). Нас презумпция достигает через критерий проверки ([1.1](#veracity)) и honor: «X mató» до приговора — ложный факт.
- **Слова чиновника не заменяют приговор.** В деле о пресс-конференции гражданского губернатора TC отказал в ампаро ([STC 244/2007](https://hj.tribunalconstitucional.es/es/Resolucion/Show/6211)), но база TC отмечает решение ЕСПЧ против Испании после него. Если министр или полиция называют человека виновным, цитируем это как их заявление и рядом называем стадию дела.
- После оправдания или прекращения обновляем описание и закреп: `operator/legal.md#gdpr-rights`, ст. 86 LOPDGDD.

### <a id="criminal-secrecy"></a>4.2 Secreto de sumario
- Материалы следствия «serán reservadas y no tendrán carácter público hasta que se abra el juicio oral» (ст. 301). Судья может объявить дело секретным «por tiempo no superior a un mes» (ст. 302); как и сколько раз это продлевают, мы не проверяли `[не проверено]`. За «revelare indebidamente» — штраф €500–10 000, в том числе для «cualquier otra persona» (ст. 301). [CP, ст. 466](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): уголовная ответственность адвокатов, чиновников и частных лиц, «que intervenga en el proceso»; журналисты прямо не названы.
- Секретность закрывает действия следствия, а не событие: «el secreto del sumario no significa, en modo alguno, que uno o varios elementos de la realidad social … sean arrebatados a la libertad de información» ([STC 13/1985, FJ 3](https://hj.tribunalconstitucional.es/es/Resolucion/Show/393)). Запрещена «revelaciones indebidas» и «conocimiento ilícito y su posterior difusión».
- Происхождение сведений и их правдивость — разные вопросы: незаконность добычи «no la transformaría en inveraz», но «la eventual responsabilidad» остаётся ([STC 54/2004, FJ 6](https://hj.tribunalconstitucional.es/es/Resolucion/Show/5059)).
- Для нас: само событие и то, что дело засекречено, сообщать можно. Материалов не запрашиваем и не покупаем. Содержание секретных материалов — только если их уже опубликовало авторитетное СМИ: с атрибуцией, без персональных данных, с пометкой об этом в досье и после решения модератора. Применяется ли штраф ст. 301 к журналисту, пересказавшему опубликованное, `[не проверено]`.

## 5. Составы о публичной речи

Основные составы и наказания — `operator/legal.md#es-criminal`. Здесь — что это значит для кадра и текста. Законопроект 122/000006 об отмене ст. 490.3, 491, 504, 525, 543 и 578 ([BOCG](https://www.congreso.es/public_oficiales/L15/CONG/BOCG/B/BOCG-15-B-19-1.PDF)) **caducado** ([Congreso](https://www.congreso.es/es/busqueda-de-iniciativas?p_p_id=iniciativas&p_p_lifecycle=0&p_p_state=normal&p_p_mode=view&_iniciativas_mode=mostrarDetalle&_iniciativas_legislatura=XV&_iniciativas_id=122%2F000006)): все эти статьи действуют.

### <a id="speech-corona"></a>5.1 Корона
- [CP, ст. 490.3](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): calumnia или injuria в связи с их функциями — prisión от 6 месяцев до 2 лет, «si la calumnia o injuria fueran graves», иначе multa 6–12 месяцев. Ст. 491.1: вне функций — multa 4–20 месяцев. Ст. 491.2: использовать образ «de cualquier forma que pueda dañar el prestigio de la Corona» — multa 6–24 месяца.
- Защищены: Король, Королева, их ascendientes и descendientes (значит, и Хуан Карлос I, и инфанты), консорт, регент, принц или принцесса Астурийские.
- Прецедент из карикатуры: обложка El Jueves с наследниками, Audiencia Nacional, 13.11.2007 — по €3 000 двум авторам по ст. 491.1 ([RSF](https://rsf.org/es/condena-de-dos-caricaturistas-de-el-jueves-3000-euros-de-multa-una-sentencia-que-relanza-el-debate)) `[вторичный]`.
- Страсбург ограничивает применение, но до суда дела доходят. Положение короля «should not shield him from all criticism» ([Otegi Mondragon v. Spain, 15.03.2011, §56](https://hudoc.echr.coe.int/eng?i=001-103951)). Сожжение фото короля как «l'expression symbolique du rejet et de la critique politique d'une institution» — нарушение ст. 10 ([Stern Taulats et Roura Capellera c. Espagne, 13.03.2018](https://hudoc.echr.coe.int/eng?i=001-181719)).
- **Можно:** новость о действиях и решениях; дословная цитата; атрибутированная критика; сцена с документом, зданием, официальным событием.
- **Нельзя:** спрайт члена семьи в унизительной, сексуальной, игровой или выдуманной сцене; деньги, наручники, символы вины рядом с ним; обложка с обвинением; голос или реплики, которых не было. Обвинение (fraude, corrupción) — только по официальному документу и с указанием стадии.

### <a id="speech-symbols"></a>5.2 Испания и символы
- [CP, ст. 543](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): «ofensas o ultrajes de palabra, por escrito o de hecho a España, a sus Comunidades Autónomas o a sus símbolos o emblemas, efectuados con publicidad» — multa 7–12 месяцев.
- TC признал наказание законным за оскорбление флага на протесте ([STC 190/2020](https://hj.tribunalconstitucional.es/es/Resolucion/Show/26541)), а ЕСПЧ по тому же делу единогласно нашёл нарушение ст. 10 ([Fragoso Dacosta v. Spain, 08.06.2023](https://hudoc.echr.coe.int/eng?i=001-225042)). Статья действует.
- Для нас: флаг как цветовой знак сюжета — можно (`format.md`). Флаг горящий, рваный, растоптанный, с шутками — только если само событие именно такое, в новостной рамке, с атрибуцией и лучше словами, а не картинкой. От лица канала оскорбительных оценок страны, автономий и их символов нет.

### <a id="speech-institutions"></a>5.3 Институты
- CP, ст. 504.1: calumnia, injuria, серьёзная угроза Правительству, CGPJ, TC, TS, совету и суду автономии — multa 12–18 месяцев. Ст. 504.2: Ejércitos, Clases o Cuerpos y Fuerzas de Seguridad. Ст. 496: Cortes в заседании. Правда защищает при условиях ст. 207, 210.
- Honor имеет «significado personalista», поэтому говорить о нём применительно к институтам «inadecuado» (STC 107/1988, FJ 2). Это защищает критику, но не отменяет ст. 504.
- Для нас: критика решения, с атрибуцией, — обычная новость. «El Tribunal X es una banda» и обобщения о «la Guardia Civil» по одному делу — нет.

### <a id="speech-hate"></a>5.4 Ненависть
- [CP, ст. 510](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444) (ред. 2022). Группы: raza, etnia, nación, origen nacional, religión, ideología, sexo, orientación e identidad, género, aporofobia, enfermedad, discapacidad.
  - 510.1.a: публичное подстрекательство «directa o indirectamente» — prisión 1–4 года, multa;
  - 510.1.b: создание и **распространение материала**, пригодного для подстрекательства;
  - 510.1.c: отрицание, грубая тривиализация или восхваление геноцида и преступлений против человечности, если это создаёт климат враждебности. Важно для роликов о войнах и истории;
  - 510.2: унижение группы или человека из-за принадлежности к ней — prisión от 6 месяцев до 2 лет;
  - 510.3: в СМИ и интернете, «accesible a un elevado número de personas», — верхняя половина наказания; 510.6: удаление контента.
- Цитирование — не автоматически состав: наказывать журналиста за передачу чужих заявлений можно лишь при особых основаниях (Jersild, цит. в STC 62/2025, FJ 2). Но пункт 510.1.b говорит о распространении, поэтому новостная рамка должна быть явной `[толкование]`.
- **Можно:** сообщить, что X заявил Y; разбор данных из официальной статистики с источником и датой; юридическая оценка суда или прокуратуры.
- **Нельзя:** длинная цитата лозунгов, которую можно вырезать как отдельный клип; обобщения о группе («los inmigrantes…») от лица канала; национальность подозреваемого без смысла для новости; стереотипные спрайты; уравнивание сторон с тривиализацией геноцида. Символику и материалы таких групп не показываем (`policy.md`).

### <a id="speech-terrorism"></a>5.5 Терроризм и унижение жертв
- [CP, ст. 578.1](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): публичное «enaltecimiento o la justificación» и действия, которые влекут «descrédito, menosprecio o humillación de las víctimas de los delitos terroristas o de sus familiares» — prisión 1–3 года и multa. 578.2: через интернет — верхняя половина. 578.4: суд обязывает удалить контент.
- TC: наказуемо как проявление discurso del odio, если создаётся «una situación de riesgo», а «la mera adhesión ideológica» защищена ([STC 112/2016, FJ 3](https://hj.tribunalconstitucional.es/es/Resolucion/Show/25026)). Суд обязан взвешивать контекст и намерение; в деле César Strawberry TC отменил приговор за твиты ([STC 35/2020](https://hj.tribunalconstitucional.es/es/Resolucion/Show/26247)). Это защита на этапе суда, а не гарантия на этапе публикации.
- **Можно:** новость о решении суда, заявлении, годовщине; атрибутированное «X reivindicó el atentado»; числа жертв с источником.
- **Нельзя:** называть участников «héroes», «mártires», «presos políticos» от лица канала (только цитата и только в рамке); пересказывать коммюнике как факт; шутки, игровые мотивы и спрайты вокруг жертв или теракта; визуал с символикой группы. Момент насилия не показываем (`policy.md`).

### <a id="speech-religion"></a>5.6 Религиозные чувства
- [CP, ст. 525.1](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): тот, кто «para ofender los sentimientos de los miembros de una confesión religiosa», публично делает «escarnio de sus dogmas, creencias, ritos o ceremonias» или унижает верующих, — multa 8–12 месяцев. Ст. 525.2: то же к неверующим. Нужна цель оскорбить `[толкование]`, поэтому нейтральная новость вне состава.
- **Можно:** новость о скандале, суде, позиции церкви; название обряда и атрибутированная критика.
- **Нельзя:** спрайты пророков, святых, таинств; шутки об обрядах; культовые символы как элемент игрового оформления; пародия на службу.

## 6. Выборы (всеобщие, 29.11.2026)

Статус: `channel.yaml` исключает предвыборные темы до 29.11.2026, поэтому раздел работает, если исключение снимут, и для пограничных роликов (новость о министре, закон, приговор политику). Короткая справка — `operator/legal.md#es-elections`; платное продвижение — `#es-ttpa`.

### <a id="election-calendar"></a>6.1 Календарь
- [RD 806/2026](https://www.boe.es/diario_boe/txt.php?id=BOE-A-2026-20742) (BOE 06.10.2026): выборы — «el domingo 29 de noviembre de 2026» (ст. 2); кампания начинается «a las cero horas del viernes 13 de noviembre» и заканчивается «a las veinticuatro horas del viernes 27 de noviembre» (ст. 4). Совпадает с [LOREG, ст. 51](https://www.boe.es/buscar/act.php?id=BOE-A-1985-11672).
- [Календарь JEC](https://www.juntaelectoralcentral.es/cs/jec/documentos/eg2026_calendario.pdf): запрет опросов — «24 a 28 de noviembre»; jornada de reflexión — 28 ноября; голосование — 29 ноября. Голосование идёт с 9 до 20 часов (ст. 84).

### <a id="election-allowed"></a>6.2 Что можно нейтральному каналу

Кампания по закону — это «actividades lícitas llevadas a cabo por los candidatos, partidos…» ради голосов (LOREG, ст. 50.4), а другие юрлица её вести не могут, «sin perjuicio» ст. 20 CE (ст. 50.5). Неагитационная новость кампанией не является. Но запрет после конца кампании действует для «cualquier persona» (JEC 25/2018): неучастие в выборах не защищает.

| Период | Нейтральная новость | Нельзя |
|---|---|---|
| **06.10–12.11** | Можно: кандидаты, партии, программы, заявления, чужие интервью, с атрибуцией | Брать у партии оплату или рекламную вставку: запрет «inserciones en prensa, radio u otros medios digitales» (ст. 53; партии не могут и «a través de tercero», [Instrucción 3/2011](https://www.juntaelectoralcentral.es/cs/jec/doctrina/instrucciones?esinstruccion=true&idacuerdoinstruccion=25785&template=Doctrina/JEC_Detalle)). Опрос без данных [6.3](#election-polls) |
| **13–23.11, кампания** | Можно то же. Стороны — с сопоставимым временем (`format.md`) | Призыв голосовать, агитация, задания от кампаний. Опрос без данных 6.3 |
| **24–27.11** | Можно то же, без опросов | Публиковать, распространять, воспроизводить опросы и их суррогаты (6.3) |
| **28.11, день размышления** | По закону можно «información objetiva», даже об актах последнего дня кампании, «siempre y cuando dicha información no entrañe propaganda electoral» (JEC 25/2018). Наше правило строже: **роликов о кандидатах не выпускаем** (`policy.md`) | Интервью с кандидатом («No cabe publicar tales entrevistas»), мнение за кого-либо, опросы, повторная публикация и закреп старых роликов о кандидатах, спрайт кандидата с его именем |
| **29.11 до 21:00 по полуострову** | Только процедура и явка из официальных источников (JEC, Interior) (`policy.md`) | То же, что 28.11: ст. 53 действует «exclusivamente a los días de reflexión y votación» (JEC 25/2018). Прогнозы и exit polls: см. ниже |
| **29.11 после 21:00** | Можно результаты и прогнозы с указанием источника | Выводы о фальсификации без официального решения |

- **Наказания.** Агитация после конца кампании: штраф (ст. 153.1) €100–1 000 для частных лиц и €300–3 000 для должностных; преступление, ст. 144.1.a — prisión от 3 месяцев до 1 года или multa 6–24 месяца.
- **Дело ABC.** Интервью с кандидаткой и её фото на первой полосе в день размышления: директору — €1 000 (JEC 25/2018), жалоба в TS отклонена (STS 905/2021, запись на той же странице JEC). Четыре члена JEC в особом мнении считали, что наказывать можно только за «mensajes directos e indubitados dirigidos a la petición de voto», так что граница спорна, но решение устояло.
- **2026 год.** Жалобы на мнение «Mañueco, for president» и на репост в X колонки «¿Por qué votar a Mañueco?», вышедшей накануне: JEC велела региональной junta решить по существу, нарушен ли ст. 53 ([93/2026](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=148652&template=Doctrina/JEC_Detalle), [91/2026](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=148650&template=Doctrina/JEC_Detalle)). Итог по этим делам нам неизвестен `[не проверено]`.
- **Рисунок кандидата.** Пост в личном X от политика, не участвующего в тех выборах: имя и рисованный силуэт лица кандидата в день размышления. Это признано пропагандой, штраф €300, хотя прямой просьбы голосовать не было ([277/2024](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=134810&template=Doctrina/JEC_Detalle), [301/2024](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=135781&template=Doctrina/JEC_Detalle)). Для пиксель-арта это прямой сигнал.
- **Что делать с уже вышедшими роликами** 28–29.11: не продвигаем и не закрепляем; нужно ли снимать, решения JEC для такого канала мы не нашли `[не проверено]`.

### <a id="election-polls"></a>6.3 Опросы (LOREG, ст. 69)
- **С 06.10 по 29.11 любая публикация опроса** должна содержать (ст. 69.1): автора и заказчика с адресом; метод выборки, размер выборки, погрешность, репрезентативность, отбор респондентов, дату полевых работ; «[t]exto íntegro de las cuestiones planteadas» и число не ответивших. Обязанность лежит на публикующем: «no es a quienes realizan las encuestas sino a quienes las publican o difunden» ([JEC 441/2023](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=120655&template=Doctrina/JEC_Detalle)).
- Режим действует для любого опроса о голосовании в период любых выборов, даже другого уровня и региона ([JEC 121/2025](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=142811&template=Doctrina/JEC_Detalle)).
- **Формат.** В JEC 441/2023 жалобы закрыты, когда данные быстро добавили со ссылкой на публикацию, и JEC одновременно призвала в будущем публиковать их сразу. Хватит ли ссылки в описании для короткого видео сразу при публикации, JEC не решала `[толкование]`. Наше правило: в кадре автор, заказчик, дата, выборка и погрешность; полный лист — в описании и на странице источников.
- **Нарушение ст. 69.1:** JEC требует исправить за 3 дня в том же месте ([JEC 269/2023](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=119361&template=Doctrina/JEC_Detalle); ст. 69.4).
- **С 24 по 28.11:** «queda prohibida la publicación y difusión o reproducción de sondeos electorales por cualquier medio de comunicación» (ст. 69.7).
  - Касается цифровых СМИ и блогов: «resultaría absurdo que lo que está prohibido en un medio escrito pueda permitirse en un medio digital» ([JEC 93/2022](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=80423&template=Doctrina/JEC_Detalle)).
  - Обход тоже считается. «EmojiPanel», где партии были блюдами, — €3 000 с учётом «limitado alcance de difusión». Там же: запрещена «cualquier proyección de intención electoral». Значит, пиксельные графики, намёки цветом и игры в этот период исключены; ставки и prediction markets приравниваем к опросу `[толкование]`.
  - Узкое исключение: можно «hacer referencia a sondeos elaborados con anterioridad», запрет толкуется ограничительно ([JEC 315/2023](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=119827&template=Doctrina/JEC_Detalle); [303/2019](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=68027&template=Doctrina/JEC_Detalle)). Граница со словом «reproducción» тонкая. Наше правило: в эти дни цифр и выводов из опросов нет вообще.
- **29.11.** Запрет ст. 69.7 формально заканчивается 28.11. Но JEC велела не передавать «estimaciones de resultados electorales» на Канары до 21:00 по полуострову, когда там закрываются участки ([458/1999](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=20597&template=Doctrina/JEC_Detalle)). Ролик в сети виден на Канарах, поэтому до 21:00 никаких прогнозов нет `[толкование]`. Опросы избирателей у выхода возможны, но вне участков ([163/2003](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=1519&template=Doctrina/JEC_Detalle)); сами мы их не проводим.
- **Опросы аудитории.** Опрос в сообществе, сторис, TikTok не нарушает закон, если среди вопросов нет того, что «directa o indirectamente, trate de averiguar la intención de voto», и по итогам нет прогнозов ([JEC 116/2026](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=149155&template=Doctrina/JEC_Detalle)). Для нас: политических опросов аудитории до 30.11 не проводим.
- **Наказания.** Штраф €3 000–30 000 (ст. 153.2). **Также преступление** (ст. 145): «[q]uienes infrinjan la normativa vigente en materia de encuestas electorales» — prisión от 3 месяцев до 1 года, multa 12–24 месяца, запрет профессии на 1–3 года. Обязанность опубликовать опровержение в 3 дня — ст. 69.4.

### <a id="election-jec-online"></a>6.4 Что JEC говорила о сети и о нашем канале
- [Instrucción 4/2007](https://www.juntaelectoralcentral.es/cs/jec/doctrina/instrucciones?esinstruccion=true&idacuerdoinstruccion=7787&template=Doctrina/JEC_Detalle): правила кампании действуют и в интернете («páginas web, foros, «chats», correo electrónico u otros medios»).
- Анонимность не защищает: об анонимном опросе в Twitter JEC попросила МВД установить автора ([465/2023](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=120879&template=Doctrina/JEC_Detalle)).
- Ст. 66.2 говорит об «emisoras de titularidad privada» и «televisiones privadas»: плюрализм и равенство, а для ТВ ещё пропорциональность и «neutralidad informativa» ([Instrucción 4/2011](https://www.juntaelectoralcentral.es/cs/jec/doctrina/instrucciones?esinstruccion=true&idacuerdoinstruccion=25790&template=Doctrina/JEC_Detalle)). Решения о том, что YouTube-канал частного лица — «emisora», мы не нашли `[не проверено]`. Но местному телеканалу с программой на YouTube JEC велела удалить оскорбление министра и «cualquier reproducción del programa en las redes sociales» (197/2024). Работаем так, будто нейтральность обязательна.
- Instrucción 2/2026 и план RTVE касаются только общественных СМИ.
- Инструкций и решений JEC об AI на выборах мы не нашли: на 04.11.2026 поиск по тексту решений («inteligencia artificial», «deepfake», «ultrasuplantación») пуст. По предмету нашёлся один документ: JEC отправила правительству доклад о законопроекте об AI ([Acuerdo 140/2026](https://www.juntaelectoralcentral.es/cs/jec/doctrina/acuerdos?esinstruccion=false&idacuerdoinstruccion=144453&template=Doctrina/JEC_Detalle)). Файл доклада требует авторизации, и его содержание мы не знаем `[ссылка не открыта]`. Пока действует общее: `operator/legal.md#ai-act-our-case`, разделы 2 и 5.

## 7. Копирайт, товарные знаки, музыка

### <a id="copyright-text"></a>7.1 Цитаты и тексты СМИ
- **В Испании нет fair use.** Свобода информации не создаёт исключения сверх перечисленных в InfoSoc: CJEU, Гранд-палата, [Spiegel Online, C-516/17](http://publications.europa.eu/resource/celex/62017CJ0516) и [Funke Medien, C-469/17](http://publications.europa.eu/resource/celex/62017CJ0469). Ссылка «мы новости» сама по себе не защита.
- **Факты и пересказ** своими словами — свободны `[толкование; общее начало]`. Защищённое изложение копировать нельзя.
- [LPI, ст. 32.1](https://www.boe.es/buscar/act.php?id=BOE-A-1996-8930): цитата опубликованной работы «a título de cita o para su análisis, comentario o juicio crítico», с указанием источника и автора. **Но:** «[t]al utilización solo podrá realizarse con fines docentes o de investigación». Покрывает ли это новостной канал, судебного ответа мы не нашли `[не проверено]`. Поэтому цитата — короткая, ради анализа, с источником и автором, и это риск, а не гарантия.
- [LPI, ст. 33.1](https://www.boe.es/buscar/act.php?id=BOE-A-1996-8930): «trabajos y artículos sobre temas de actualidad» могут воспроизводить другие СМИ «de la misma clase», с источником, если в оригинале нет «reserva de derechos», при этом автору полагается вознаграждение. Для нас почти бесполезна: оговорка прав есть почти везде, и мы вряд ли «misma clase» `[толкование]`.
- Ст. 33.2: выступления, «informes ante los Tribunales» можно передавать «con el exclusivo fin de informar sobre la actualidad». Речи в Cortes и в муниципальных советах — без этого условия. Опора для цитат политиков.
- Ст. 13: законы, решения судов, акты органов не охраняются. Цитируем документы первоисточника свободно; персональные данные — отдельно.
- Ст. 35.1: то, что видно или слышно при событии, — только «en la medida que lo justifique dicha finalidad informativa». Для нас неактуально (своих съёмок нет).

### <a id="copyright-press"></a>7.2 Право издателей (ст. 129 bis)
- Введено RDL 24/2021: издатели и агентства, «establecidas en el territorio español», имеют исключительное право на воспроизведение и доведение своих публикаций до публики «por parte de prestadores de servicios de la sociedad de la información» ([LPI, ст. 129 bis.1](https://www.boe.es/buscar/act.php?id=BOE-A-1996-8930)). Для «terceros usuarios» текста или фото из такой публикации нужна авторизация (129 bis.2).
- Не применяется к ссылкам, «palabras sueltas o extractos muy breves», если это не вредит инвестициям издателя, и к содержимому под исключениями (129 bis.6).
- Считаемся ли мы «prestador» — скорее да: LSSI применяется к сайту источников (`operator/legal.md#lssi`), а канал на площадке `[толкование]`.
- Для нас: ссылка на статью — свободна. Заголовок в плашке — короткая цитата. Скриншотов статей, копий лида и фото из СМИ не делаем. Название источника пишем текстом (`format.md`).

### <a id="copyright-images"></a>7.3 Фото, видео, AI-перерисовка
- Пресс-фото и видео третьих лиц не используем (`policy.md`). Даже обычный снимок охраняется 25 лет как «mera fotografía» ([LPI, ст. 128](https://www.boe.es/buscar/act.php?id=BOE-A-1996-8930)); ст. 33 говорит о «trabajos y artículos», а не о фото `[толкование]`.
- Перерисовка конкретного снимка (img2img, референс) может быть переработкой чужого произведения `[толкование]`; генерируем только по описанию (`policy.md`).
- [CP, ст. 270](https://www.boe.es/buscar/act.php?id=BOE-A-1995-25444): копирование с «ánimo de obtener un beneficio económico directo o indirecto» может быть преступлением — prisión от 6 месяцев до 4 лет. Монетизированный канал этой цели не отрицает.
- Автор — «la persona natural» (LPI, ст. 5.1). Полностью сгенерированное, скорее всего, нашим авторским правом не защищено `[толкование]`: не заявляем на него права и не регистрируем в Content ID.

### <a id="copyright-trademarks"></a>7.4 Логотипы и знаки
- [Ley 17/2001, ст. 37.1.c](https://www.boe.es/buscar/act.php?id=BOE-A-2001-23093): марку можно использовать, чтобы «designar productos o servicios como correspondientes al titular» или сослаться на них, если это соответствует «prácticas leales» (37.2).
- Для нас: логотипы СМИ, партий и компаний не ставим, название пишем текстом (`format.md`). Логотип партии в кадре в период выборов похож и на агитацию ([6.2](#election-allowed)). Брендинг и плашки под реальные СМИ, спрайты и темы игр запрещены (`policy.md`; основания — `countries/us/legal.md#copyright-trademarks`).

### <a id="music"></a>7.5 Музыка
- Подход единый для всех каналов: только треки из каталога с лицензией на площадку (`music/catalog.yaml`, `docs/architecture.md` §14; разбор прав — `countries/us/legal.md#music`). Трек вне каталога — замечание; трек из каталога со `status: approved` — принятый риск (`newsroom/teams/legal/_common.md`).
- Испанская специфика: нужно ли разрешение общества по управлению правами (SGAE и др.) для каталога CC0 или royalty-free на испанском канале, мы не проверяли `[не проверено]`. Вопрос юристу до первых выплат.
- Испанских песен, мелодий, гимнов под сюжет не берём. В ролике о трагедии музыки нет (`format.md`, серьёзный режим).

## 8. Регуляторы и раскрытие

### <a id="regulatory-minors"></a>8.1 «Not made for kids»
- Канал и ролики — «not made for kids». Причина: темы (политика, преступления, войны) адресованы взрослым, а контент для детей подчинил бы канал иным правилам рекламы и данных.
- YouTube требует указывать аудиторию вне зависимости от страны: «[i]ndependientemente de tu ubicación, debes comunicarnos si tus vídeos se pueden considerar contenido creado para niños», со ссылкой на COPPA «y/u otras leyes» ([YouTube Help](https://support.google.com/youtube/answer/9527654?hl=es)). Критерии — `countries/us/legal.md#regulatory-coppa`; мультяшные персонажи — фактор риска (`platforms/youtube/rules.md`). Настройку ставим на каждом ролике.
- Отдельной испанской категории «детский контент» для обычного блогера мы не нашли `[не проверено]`. Близкие нормы: согласие на обработку данных с 14 лет ([LOPDGDD, ст. 7.1](https://www.boe.es/buscar/act.php?id=BOE-A-2018-16673)); после порогов — предупреждения и символы возраста (ст. 94.1, 99 [LGCA](https://www.boe.es/buscar/act.php?id=BOE-A-2022-11311), `operator/legal.md#lgca-duties`).
- Для нас: 8-битная эстетика создаёт риск «детской» классификации. Ведущие взрослые, никаких «para niños», детских тегов и маскотов.

### <a id="regulatory-ads"></a>8.2 Реклама и спонсоры
Сейчас канал рекламу не интегрирует. Требования появляются с первым спонсором и действуют **до порогов LGCA**:
- [Ley 34/1988, ст. 9](https://www.boe.es/buscar/act.php?id=BOE-A-1988-26156): СМИ «deslindarán perceptiblemente» информацию от рекламы, а реклама раскрывает свой характер «inequívocamente».
- [Ley 3/1991, ст. 26.1](https://www.boe.es/buscar/act.php?id=BOE-A-1991-628): оплаченная реклама, поданная как информация, в том числе в соцсетях, — недобросовестная практика, если это не ясно «en el contenido, o a través de imágenes y sonidos claramente identificables».
- [LSSI, ст. 20.1](https://www.boe.es/buscar/act.php?id=BOE-A-2002-13758): электронные коммерческие сообщения должны быть «claramente identificables», и заказчик тоже.
- После порогов: LGCA, ст. 94.1, 121–134, санкции CNMC за отсутствие «publicidad» или «publi» (`operator/legal.md#lgca-duties`, `#lgca-fines`).
- Кодекс [AUTOCONTROL (2025)](https://www.autocontrol.es/app/uploads/codigo-de-conducta-de-publicidad-a-traves-de-influencers-2025.pdf), самоурегулирование, не закон: подходят «publicidad», «publi», «en colaboración con», «colaboración pagada», «patrocinado por». **Не подходят** «Colab», «Sponso» и «indicaciones en un idioma distinto al de la comunicación tales como “Ad” o “Advertising”». Метка — в начале сообщения, заметно. AI-рекламу кодекс велит не делать вводящей в заблуждение (п. 11).
- Поэтому в `policy.md` — «Publicidad» в кадре и голосом плюс инструмент площадки.
- Новость за деньги и «темы» от заказчика мы не берём (`policy.md`). Политическая реклама: `operator/legal.md#es-ttpa`, [6.2](#election-allowed).

### <a id="ai-disclosure"></a>8.3 Маркировка AI и строка «Voz IA · Ilustración IA»
- Правовая база и выводы — `operator/legal.md#ai-act-art50`, `#ai-act-our-case`, `#ai-act-code`.
- Текст [AI Act на испанском](http://data.europa.eu/eli/reg/2024/1689/oj), ст. 50.4: deployer «harán público que estos contenidos o imágenes han sido generados o manipulados de manera artificial». Ст. 50.5: «de manera clara y distinguible a más tardar con ocasión de la primera interacción o exposición», с учётом доступности. **Конкретных слов закон не задаёт.** Deep fake в испанском тексте — «ultrasuplantación».
- **Подходит ли строка `[толкование]`:** да, если она видна с первого кадра и читается. «IA» — официальный испанский термин, его использует и сам регламент, и [AESIA](https://aesia.digital.gob.es/storage/media/normas-de-transparencia-datos-clave.pdf); зрителю понятно, что сгенерировано (голос, изображение). «Voz IA» и «Ilustración IA» сильнее общего флага, потому что называют, что именно создано AI.
- **Отступление от Code of Practice.** [Code](https://ec.europa.eu/newsroom/dae/redirection/document/129555), Measure 1.1.a: главный элемент метки — «the capitalised acronym “AI” in the English language», если это не противоречит национальным законам о языке, «in which case the acronym may be disclosed in the national language». Закон о языке, запрещающий здесь английский, мы не искали `[не проверено]`. Code добровольный, поэтому «IA» не нарушает ст. 50, но и соответствия Code эта строка сама по себе не показывает. Решение владельца: оставить «IA» (понятнее аудитории) или добавить в начале ролика маленькую иконку «AI». Обсудить с юристом.
- **Что проверяет ревьюер:**
  - строка с первого кадра до конца: «Voz IA»; пока реальный человек в кадре — «Voz IA · Ilustración IA»; на обложке — «Ilustración IA · Texto IA» (`format.md`);
  - читается на телефоне: мелкий серый шрифт на чёрной полосе — на границе «clara y distinguible» `[толкование]`. Если на кадре не читается, это замечание;
  - вшитый текст недоступен экранным дикторам, поэтому в описании одна строка: «Vídeo con voz e ilustraciones generadas con IA.»;
  - не вводит в заблуждение: AI-метка не означает, что можно показать то, чего не было ([2.2](#likeness-scenes)); Code прямо говорит, что метка «does not exempt them» от других законов;
  - флаги площадок (`platforms/*/rules.md`).

### <a id="required-disclosures"></a>8.4 Раскрытия, которых требует закон (для `required_disclosures`)
- Всегда: AI-голос и AI-изображение — строка из [8.3](#ai-disclosure), флаг площадки.
- При AI-тексте об общественно важном: раскрытие по ст. 50.4, абз. 2. Исключением human review не пользуемся с 09.11.2026 (`operator/legal.md#ai-act-our-case`).
- При опросе в период 06.10–29.11: данные ст. 69.1 LOREG ([6.3](#election-polls)). С 24 по 28.11 опросов нет, раскрывать нечего.
- При спонсоре или оплате: «Publicidad» в кадре и голосом, инструмент площадки ([8.2](#regulatory-ads)).
- При требовании опровержения: текст целиком, бесплатно, без комментариев, в 3 дня (`operator/legal.md#es-rectification`). Требование JEC о поправке опроса: с указанием, что это по её требованию, в 3 дня (ст. 69.4).
- При новых сведениях о деле (оправдание, прекращение): «aviso de actualización» по запросу (ст. 86 LOPDGDD, `operator/legal.md#gdpr-rights`).
- При наличии судебного запрета о личности жертвы: не раскрытие, а запрет, см. [3.3](#victims).
- После порогов LGCA: возрастные предупреждения, ст. 99.1 и 99.4 (`operator/legal.md#lgca-duties`).
- На сайте источников: aviso legal (`operator/legal.md#lssi`).

## <a id="red-flags"></a>9. Красные флаги для ревьюера

Серьёзность — ориентир по шкале Legal (`newsroom/teams/legal/_common.md`). Флаги оператора — в `operator/legal.md#red-flags`.

| Формулировка или элемент | Правовой риск: кто и что может предъявить | Как исправить |
|---|---|---|
| «X mató / robó» до приговора; «el asesino X», «el presunto asesino X» | **blocker** · X: honor, calumnia; максимальный долг проверки ([1.1](#veracity), [4.1](#criminal-terms)) | «X, acusado de…», стадия и документ |
| «Imputado» или стадия не совпадает с досье (detenido → «investigado», investigado → «acusado») | **high** · X: ложный статус дела ([4.1](#criminal-terms)) | сверить стадию по таблице |
| «Condenado» без «no es firme», когда приговор не окончательный; «archivado» как «inocente» | **high** · ложный статус ([4.1](#criminal-terms)) | назвать стадию точно: «no es firme», «provisionalmente» |
| Статус устарел: оправдан, прекращено | **high** · X: rectificación, ст. 86 LOPDGDD ([4.1](#criminal-terms)) | обновить описание и закреп, порядок в operator |
| Чужое обвинение от анонимного источника или в пересказе СМИ | **blocker** · X: honor; reportaje neutral не применяется ([1.3](#reportaje-neutral)) | документ или независимые подтверждения; иначе убрать |
| Ролик опирается на «как сообщило агентство» и не проверял | **high** · X: долг проверки не снимается (STC 62/2025) ([1.3](#reportaje-neutral)) | контраст, первоисточник; пометка в досье |
| Обвинение или вопрос-намёк в хуке, заголовке, обложке | **blocker** · X: STC 54/2004 ([1.3](#reportaje-neutral)) | факт с источником; без вопроса |
| Цитата подрезана, склеена, соседствует с вердиктом от канала | **high** · X: потеря нейтральности ([1.3](#reportaje-neutral)) | дословно, с автором, без оценки |
| Оценка с фактическим ядром от лица канала («corrupto», «ladrón») | **high** · X: injuria, honor ([1.2](#opinion-fact)) | атрибутировать; факты с источником |
| Спрайт не того человека, образ рядом с чужим преступлением | **blocker** · X: honor, imagen; «inveraz» при верном тексте ([2.2](#likeness-scenes)) | сверить имя и образ; нейтральная сцена |
| Реальный человек в сцене, которой нет в досье (арест, наручники, встреча) | **blocker** · X: honor; AESIA: deep fake ([2.2](#likeness-scenes)) | только документированные события |
| Спрайт частного лица, агента, анонимного служащего | **high** · герой: imagen, 8.2 не применяется ([2.1](#likeness-caricature)) | обезличенная схема |
| Образ реального человека в рекламе, мерче, продвижении канала | **high** · человек: ст. 7.6 ([2.1](#likeness-caricature), [2.3](#likeness-voice)) | убрать |
| Голос реального человека, клон, имитация | **blocker** · человек: ст. 7.6; AESIA ([2.3](#likeness-voice)) | читает ведущий с атрибуцией |
| Личность или узнаваемые детали жертвы сексуального насилия, ребёнка, их родных | **blocker** · LECrim 681.3; Fiscal; AEPD ([3.2](#minors), [3.3](#victims)) | обезличить, убрать детали |
| Образ жертвы гендерного насилия, натуралистичная подача | **high** · LO 1/2004, ст. 14 ([3.3](#victims)) | символ или место; серьёзный режим |
| Секретные материалы дела без публикации в авторитетном СМИ | **high** · ст. 301 LECrim; X ([4.2](#criminal-secrecy)) | не использовать; сообщить о секретности |
| Король или семья в унижающей, игровой, выдуманной сцене; денежные символы рядом | **blocker** · Fiscal: CP 490.3, 491.2 ([5.1](#speech-corona)) | нейтральная сцена, цитата, атрибуция |
| Флаг или символ горит, растоптан, с шуткой, не как сам факт события | **high** · Fiscal: CP 543 ([5.2](#speech-symbols)) | словами, в рамке новости |
| Обобщения о группе от канала; стереотипные спрайты; длинные лозунги ненависти; тривиализация геноцида | **blocker** · Fiscal: CP 510 ([5.4](#speech-hate)) | атрибуция, факты, короткая цитата |
| «Héroe», «mártir» о террористе от канала; шутки о жертвах; коммюнике как факт | **blocker** · Fiscal: CP 578 ([5.5](#speech-terrorism)) | новостная рамка, атрибуция |
| Спрайт пророка, таинства; шутка об обряде | **high** · CP 525 ([5.6](#speech-religion)) | нейтральное описание |
| Опрос без данных ст. 69.1 с 06.10 | **high** · JEC ([6.3](#election-polls)) | добавить данные или убрать опрос |
| Опрос, прогноз, «шуточный рейтинг», цветовой намёк, ставки 24–28.11 | **blocker** · JEC: €3 000–30 000; ст. 145 ([6.3](#election-polls)) | убрать, не вести как игру |
| Прогноз или результат 29.11 до 21:00 по полуострову | **high** · JEC 458/1999 ([6.3](#election-polls)) | перенести |
| Ролик, репост, закреп о кандидате, спрайт с именем 28–29.11 | **high** · JEC: ст. 53, 144.1.a ([6.2](#election-allowed)) | перенести на 30.11 |
| Вопрос аудитории о том, за кого голосовать | **high** · JEC 116/2026 ([6.3](#election-polls)) | не проводить до 30.11 |
| Оскорбление политика в кадре в период выборов | **high** · JEC 197/2024; injuria ([6.4](#election-jec-online), [1.2](#opinion-fact)) | убрать |
| Длинная цитата, перевод статьи, скриншот с фото | **high** · издатель: LPI, ст. 32, 129 bis ([7.1](#copyright-text), [7.2](#copyright-press)) | пересказ, короткая цитата, ссылка |
| Логотип СМИ, партии, оформление «как у RTVE» | **high** · владелец знака; выборы ([7.4](#copyright-trademarks)) | название текстом |
| Трек вне каталога или лицензия не на площадку | **blocker** · правообладатель ([7.5](#music)) | только `music/catalog.yaml` |
| Детская подача; неверная настройка аудитории | **high** · YouTube; COPPA ([8.1](#regulatory-minors)) | «not made for kids», взрослая подача |
| Спонсор без «Publicidad», метка «Ad» на испанском канале | **high** · LGP, LCD, LSSI; CNMC ([8.2](#regulatory-ads)) | «Publicidad» в кадре и голосом, инструмент площадки |
| Нет строки «Voz IA», строка нечитаема, нет «Ilustración IA» при реальном человеке | **high** · AESIA: ст. 50 ([8.3](#ai-disclosure)) | вернуть строку, поднять контраст, флаг |
| Требование опровержения, запрос об удалении, письмо JEC | **blocker** · сроки 3 дня и месяц (`operator/legal.md#es-rectification`, [8.4](#required-disclosures)) | копии и юрист в тот же день |
