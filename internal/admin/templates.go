package admin

import "html/template"

type signInPage struct {
	Message string
}

type loginPage struct {
	Token string
	Name  string
}

const head = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<style>
:root { --bg: #fbfaf7; --panel: #fff; --fg: #1d1d1f; --muted: #62666c; --rule: #e3e0d8; --accent: #1f5fbf; --ok: #1a7f37; --wait: #9a6700; --bad: #b42318; --off: #62666c; --flash: #e9f5ec; --flash-bad: #fbeae8; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #15161a; --panel: #1d1f24; --fg: #ececec; --muted: #a0a4ab; --rule: #2f3238; --accent: #8ab4f8; --ok: #6dd58c; --wait: #e3b341; --bad: #ff8b7e; --off: #a0a4ab; --flash: #18301f; --flash-bad: #3a1d1a; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 16px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
main { max-width: 860px; margin: 0 auto; padding: 16px 16px 56px; }
header { display: flex; flex-wrap: wrap; gap: 8px 16px; align-items: center; justify-content: space-between; margin-bottom: 12px; }
header .brand { font: 600 14px/1 ui-monospace, Menlo, Consolas, monospace; letter-spacing: .06em; text-transform: uppercase; color: var(--muted); }
header .brand a { color: inherit; text-decoration: none; }
.who { color: var(--muted); font-size: 14px; display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
h1 { font-size: 22px; line-height: 1.25; margin: 4px 0 6px; overflow-wrap: anywhere; }
h2 { font-size: 18px; margin: 28px 0 2px; }
.hint { color: var(--muted); font-size: 14px; margin: 0 0 10px; }
a { color: var(--accent); overflow-wrap: anywhere; }
.tabs { display: flex; flex-wrap: wrap; gap: 8px; margin: 0 0 8px; }
.tabs a { padding: 6px 12px; border: 1px solid var(--rule); border-radius: 999px; text-decoration: none; color: var(--fg); font-size: 14px; }
.tabs a.on { background: var(--fg); color: var(--bg); border-color: var(--fg); }
.flash { padding: 10px 12px; border-radius: 8px; background: var(--flash); margin: 8px 0; }
.flash.bad { background: var(--flash-bad); }
.card { display: flex; gap: 12px; padding: 12px; background: var(--panel); border: 1px solid var(--rule); border-radius: 10px; margin: 0 0 10px; }
.cover { flex: none; width: 72px; height: 128px; border-radius: 6px; background: var(--rule); object-fit: cover; display: block; }
.body { min-width: 0; flex: 1; }
.title { font-weight: 600; text-decoration: none; color: var(--fg); overflow-wrap: anywhere; }
.meta { color: var(--muted); font-size: 14px; margin: 2px 0 6px; overflow-wrap: anywhere; }
.badge { display: inline-block; font-size: 13px; font-weight: 600; }
.badge.ok { color: var(--ok); } .badge.live { color: var(--ok); } .badge.wait { color: var(--wait); } .badge.bad { color: var(--bad); } .badge.off { color: var(--off); }
.row { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; margin-top: 8px; }
form { margin: 0; }
form.inline { display: inline; }
button { font: inherit; font-size: 14px; padding: 7px 12px; min-height: 36px; border-radius: 8px; border: 1px solid var(--rule); background: var(--panel); color: var(--fg); cursor: pointer; }
button.go { background: var(--accent); border-color: var(--accent); color: #fff; }
@media (prefers-color-scheme: dark) { button.go { color: #10131a; } }
button.link { border: 0; background: none; color: var(--accent); padding: 0; min-height: 0; text-decoration: underline; }
input[type=text], input[type=url], textarea { font: inherit; font-size: 16px; width: 100%; padding: 7px 10px; border-radius: 8px; border: 1px solid var(--rule); background: var(--bg); color: var(--fg); }
textarea { min-height: 72px; margin: 6px 0; }
.link-form { display: flex; gap: 8px; margin-top: 8px; }
.link-form input { flex: 1; min-width: 0; }
.state-visible { color: var(--ok); } .state-hidden { color: var(--wait); }
details { margin-top: 8px; }
summary { cursor: pointer; color: var(--muted); font-size: 14px; }
.player { display: flex; flex-wrap: wrap; gap: 12px; margin: 12px 0; }
video { width: 260px; max-width: 100%; aspect-ratio: 9 / 16; background: #000; border-radius: 8px; }
.poster { width: 146px; aspect-ratio: 9 / 16; border-radius: 8px; object-fit: cover; }
.field { margin: 14px 0; }
.field h3 { margin: 0 0 4px; font-size: 14px; font-weight: 600; display: flex; justify-content: space-between; gap: 8px; align-items: center; }
pre { margin: 0; padding: 10px 12px; background: var(--panel); border: 1px solid var(--rule); border-radius: 8px; white-space: pre-wrap; overflow-wrap: anywhere; font-size: 14px; line-height: 1.45; }
ul.plain { margin: 4px 0; padding-left: 18px; font-size: 14px; }
.warn { color: var(--bad); }
.progress { padding: 8px 0; border-top: 1px solid var(--rule); font-size: 14px; overflow-wrap: anywhere; }
</style>
`

const topBar = `<header>
<div class="brand"><a href="/admin">8BitNews · админка</a></div>
<div class="who">{{.Who}}<form class="inline" method="post" action="/admin/logout"><button class="link" type="submit" title="Выйти на всех устройствах">Выйти</button></form></div>
</header>
{{if .Flash}}<div class="flash{{if .FlashBad}} bad{{end}}" role="status">{{.Flash}}</div>{{end}}
`

var signInTemplate = template.Must(template.New("signin").Parse(head + `<title>Вход · 8BitNews</title>
</head>
<body><main>
<h1>Вход в админку</h1>
{{with .Data.Message}}<div class="flash bad" role="alert">{{.}}</div>{{end}}
<p>Напишите <b>/admin</b> боту <a href="https://t.me/EightBitNewsBot">@EightBitNewsBot</a> в личные сообщения. Он пришлёт ссылку для входа. Пароля нет, вход только для администраторов группы модерации.</p>
</main></body></html>
`))

var loginTemplate = template.Must(template.New("login").Parse(head + `<title>Вход · 8BitNews</title>
</head>
<body><main>
<h1>Вход в админку</h1>
<p>Войти как {{.Data.Name}}?</p>
<form method="post" action="/admin/login"><input type="hidden" name="t" value="{{.Data.Token}}"><button class="go" type="submit">Войти</button></form>
</main></body></html>
`))

// The quick buttons of a row. An approval in the list is one press, as on the
// card; a reason, the link's removal and closing the sources page live on the
// video's own page.
const rowCard = `{{define "row"}}<div class="card">
{{if .CoverKey}}<a href="/admin/r/{{.ID}}"><img class="cover" loading="lazy" alt="" src="/admin/media/{{.ID}}/cover"></a>{{end}}
<div class="body">
<a class="title" href="/admin/r/{{.ID}}">{{.DisplayTitle}}</a>
<div class="meta"><span class="badge {{.StatusClass}}">{{.StatusText}}</span> · {{.ChannelID}} · {{.When}}{{with .Duration}} · {{.}}{{end}}</div>
{{if .YouTubeURL}}<div class="meta"><a href="{{.YouTubeURL}}">Открыть на YouTube</a>{{if eq .VideoState "visible"}} · <span class="state-visible">виден по ссылке</span>{{else if eq .VideoState "hidden"}} · <span class="state-hidden">скрыт или удалён</span>{{end}}{{with .Sources}} · <a href="{{.}}">Источники</a>{{end}}</div>{{else if .Sources}}<div class="meta"><a href="{{.Sources}}">Источники</a></div>{{end}}
<div class="row">
{{if eq .Status "pending_approval"}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="back" value="list"><input type="hidden" name="action" value="approve"><button class="go" type="submit">✅ Одобрить</button></form><a href="/admin/r/{{.ID}}">Смотреть, вернуть или отклонить</a>{{end}}
{{if and .Barred (or (eq .Status "approved") (eq .Status "published"))}}<span class="warn">Legal не допустил YouTube.{{with .Allowed}} Допущены: {{.}}.{{end}}</span>{{end}}
{{with .PublishHold}}<span class="warn">Задержан: {{.}}</span>{{end}}
{{if eq .Status "approved"}}{{if and .YouTubeURL (not .AutoPublishes)}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="back" value="list"><input type="hidden" name="action" value="published"><button class="go" type="submit">🟢 Вышел</button></form>{{end}}<a href="/admin/r/{{.ID}}">Пакет для загрузки</a>{{end}}
</div>
{{if .NeedsLink}}<form class="link-form" method="post" action="/admin/r/{{.ID}}/link"><input type="hidden" name="back" value="list"><input type="text" name="url" placeholder="Ссылка на ролик на YouTube" aria-label="Ссылка на ролик на YouTube" required><button type="submit">Сохранить</button></form>{{end}}
</div>
</div>{{end}}`

var listTemplate = template.Must(template.New("list").Parse(rowCard + head + `<title>Ролики · 8BitNews</title>
</head>
<body><main>
{{with .Data}}` + topBar + `
<nav class="tabs" aria-label="Каналы">{{range .Tabs}}<a href="{{.URL}}"{{if .Active}} class="on" aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
{{if .Empty}}<p>Роликов пока нет.</p>{{end}}
{{range .Sections}}<h2>{{.Title}} · {{len .Rows}}</h2>
{{with .Hint}}<p class="hint">{{.}}</p>{{end}}
{{range .Rows}}{{template "row" .}}{{end}}
{{end}}
{{with .InProgress}}<h2>В работе · {{len .}}</h2>
<p class="hint">Сюжет выбран, ролик ещё не сдан. Стадию hub не видит; прогон мог и остановиться.</p>
{{range .}}<div class="progress"><b>{{.StoryTitle}}</b><div class="meta">{{.ChannelID}} · {{.When}}</div></div>{{end}}
{{end}}
{{with .Closed}}<details><summary>Закрытые · {{len .}}</summary>
{{range .}}{{template "row" .}}{{end}}
</details>{{end}}
{{end}}
</main></body></html>
`))

var detailTemplate = template.Must(template.New("detail").Parse(head + `<title>{{.Data.Title}} · 8BitNews</title>
</head>
<body><main>
{{with .Data}}` + topBar + `
<p class="meta"><a href="/admin">← Все ролики</a></p>
<h1>{{.Title}}</h1>
<div class="meta"><span class="badge {{.StatusClass}}">{{.StatusText}}</span> · {{.Channel}} · {{.When}}{{if .ModeratedBy}} · решение: {{.ModeratedBy}}{{with .ModeratedAt}}, {{.}}{{end}}{{end}}</div>
{{with .Note}}<p><b>Причина:</b> {{.}}</p>{{end}}
<div class="player">
{{if .HasVideo}}<video controls preload="metadata" src="/admin/media/{{.ID}}/video"{{if .HasCover}} poster="/admin/media/{{.ID}}/cover"{{end}}></video>{{end}}
{{if .HasCover}}<a href="/admin/media/{{.ID}}/cover" aria-label="Открыть обложку"><img class="poster" alt="Обложка" src="/admin/media/{{.ID}}/cover"></a>{{end}}
</div>

{{if .CanDecide}}<div class="row">
<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="approve"><button class="go" type="submit">✅ Одобрить</button></form>
</div>
<details><summary>✏️ Вернуть на доработку</summary><form method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="changes"><textarea name="note" placeholder="Что доработать" aria-label="Что доработать" required></textarea><button type="submit">Вернуть</button></form></details>
<details><summary>❌ Отклонить</summary><form method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="reject"><textarea name="note" placeholder="Почему" aria-label="Почему отклонено" required></textarea><button type="submit">Отклонить</button></form></details>
{{end}}

{{if .Barred}}<p class="warn">Legal не допустил этот ролик на YouTube.{{with .Allowed}} Допущены: {{.}}.{{else}} Не допущена ни одна площадка.{{end}}</p>
{{if or .CanPublish .IsPublished}}<div class="row">{{if .CanPublish}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="published"><button type="submit">🟢 Вышел на площадках</button></form>{{end}}{{if .IsPublished}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="unpublished"><button type="submit">↩️ Не вышел</button></form>{{end}}{{with .Sources}}<a href="{{.}}">Страница источников</a>{{end}}</div>
<details><summary>🚫 Закрыть страницу источников</summary><p class="hint">Ошибочное одобрение или претензия. Обратной кнопки нет. Сам ролик на площадках снимите вручную.</p><form method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="withdraw"><textarea name="note" placeholder="Почему (необязательно)" aria-label="Почему закрываете"></textarea><button type="submit">Закрыть страницу</button></form></details>{{end}}
{{end}}

{{if .CanLink}}<h2>YouTube</h2>
{{if .Link}}<p class="meta"><a href="{{.Link}}">{{.Link}}</a>{{if eq .VideoState "visible"}} · <span class="state-visible">виден по ссылке</span>{{else if eq .VideoState "hidden"}} · <span class="state-hidden">скрыт или удалён</span>{{end}}</p>{{end}}
<form class="link-form" method="post" action="/admin/r/{{.ID}}/link"><input type="text" name="url" value="{{.Link}}" placeholder="Ссылка на ролик на YouTube" aria-label="Ссылка на ролик на YouTube"><button type="submit">Сохранить</button></form>
<div class="row">
{{if .CanPublish}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="published"><button class="go" type="submit">🟢 Вышел на площадках</button></form>{{end}}
{{if .IsPublished}}<form class="inline" method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="unpublished"><button type="submit">↩️ Не вышел</button></form>{{end}}
{{with .Sources}}<a href="{{.}}">Страница источников</a>{{end}}
</div>
<details><summary>🚫 Закрыть страницу источников</summary><p class="hint">Ошибочное одобрение или претензия. Обратной кнопки нет. Сам ролик на площадках снимите вручную.</p><form method="post" action="/admin/r/{{.ID}}/act"><input type="hidden" name="action" value="withdraw"><textarea name="note" placeholder="Почему (необязательно)" aria-label="Почему закрываете"></textarea><button type="submit">Закрыть страницу</button></form></details>
{{end}}

<h2>Сюжет</h2>
<p class="meta">{{.StoryTitle}}</p>
{{with .Development}}<p>{{.}}</p>{{end}}
<p class="meta">Источников {{.SourceCount}}, в архиве {{.Archived}}</p>

{{with .Reports}}<h2>Заметки QA и Legal</h2><pre>{{.}}</pre>{{end}}

{{with .PackageErr}}<p class="warn">{{.}}</p>{{end}}
{{range .Warnings}}<p class="warn">⚠️ {{.}}</p>{{end}}
{{range .Notes}}<p class="hint">{{.}}</p>{{end}}
{{with .Package}}<h2>Пакет для загрузки на YouTube</h2>
<div class="row">{{if $.Data.HasUpload}}<a href="/admin/media/{{$.Data.ID}}/upload">Видео для загрузки</a>{{end}}{{if $.Data.HasCover}}<a href="/admin/media/{{$.Data.ID}}/cover">Обложка</a>{{end}}</div>
<div class="field"><h3>Название <button type="button" data-copy="p-title">Копировать</button></h3><pre id="p-title">{{.Title}}</pre></div>
<div class="field"><h3>Описание <button type="button" data-copy="p-desc">Копировать</button></h3><pre id="p-desc">{{.Description}}</pre></div>
{{if $.Data.TagsLine}}<div class="field"><h3>Теги <button type="button" data-copy="p-tags">Копировать</button></h3><pre id="p-tags">{{$.Data.TagsLine}}</pre></div>{{end}}
{{with .Settings}}<div class="field"><h3>Настройки</h3><ul class="plain">{{range .}}<li>{{.}}</li>{{end}}</ul></div>{{end}}
{{end}}
{{end}}
<script nonce="{{.Nonce}}">
document.addEventListener('click', function (e) {
  var b = e.target.closest('[data-copy]');
  if (!b) return;
  navigator.clipboard.writeText(document.getElementById(b.dataset.copy).textContent).then(function () {
    b.textContent = 'Скопировано';
    setTimeout(function () { b.textContent = 'Копировать'; }, 1500);
  }, function () { b.textContent = 'Не вышло'; });
});
</script>
</main></body></html>
`))
