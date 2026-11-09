package publish

import (
	"html/template"
	"net/http"
)

// The privacy page: what Google's OAuth consent screen links to, and the public
// notice GDPR art. 14 asks for (operator/legal.md#gdpr-rights). One page in both
// languages of the channels.

type privacyPage struct {
	Contact string
	T       pageWords
}

func (p *Pages) privacyPage(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, privacyTemplate, privacyPage{Contact: p.cfg.CorrectionsContact, T: wordsFor("en-US")})
}

var privacyTemplate = template.Must(template.New("privacy").Parse(pageHead + `<title>Privacy | 8BitNews</title>
</head>
<body><main>
<div class="brand">8BitNews</div>
<h1>Privacy</h1>
<p>8BitNews makes short news videos with AI and publishes them on YouTube. It is run from Spain by the owner of its channels, who decides what happens to the data described here.{{if .Contact}} Contact: {{.Contact}}.{{end}}</p>
<h2>People named in our videos</h2>
<p>To check a story we collect what public sources say: news articles, official documents, statements. That can include the names, public roles and public statements of people in the story. We use this for news reporting, and we keep it as the record of how a video was checked. The sources of each video are listed on its page. To ask for a correction or an update, or to object, write to the contact above. We answer within a month.</p>
<h2>Visitors to these pages</h2>
<p>These public pages use no analytics, no third-party scripts and no cookies. The web server may keep technical logs, including IP addresses, to run and protect the site.</p>
<h2>Google and YouTube accounts</h2>
<p>The app “8bitnews publisher” uses YouTube API Services to upload videos to the owner's own YouTube channels, to manage those videos and to read those channels' statistics. It works only with channels the owner has connected, and it holds nothing about individual viewers or any other account. It stores the token Google issues, encrypted, with the channel's id and name. These are shared with no one, and they are deleted when the owner disconnects the channel. Access can also be withdrawn at <a href="https://myaccount.google.com/permissions">Google's permissions page</a>. See also the <a href="https://www.youtube.com/t/terms">YouTube Terms of Service</a> and the <a href="https://policies.google.com/privacy">Google Privacy Policy</a>.</p>
<hr>
<h1 lang="es">Privacidad</h1>
<div lang="es">
<p>8BitNews hace vídeos breves de noticias con IA y los publica en YouTube. Lo gestiona desde España el titular de sus canales, que decide qué se hace con los datos descritos aquí.{{if .Contact}} Contacto: {{.Contact}}.{{end}}</p>
<h2>Personas citadas en nuestros vídeos</h2>
<p>Para comprobar una noticia recogemos lo que dicen fuentes públicas: artículos, documentos oficiales, declaraciones. Eso puede incluir el nombre, el cargo público y las declaraciones públicas de personas que aparecen en la noticia. Lo usamos para informar y lo conservamos como constancia de cómo se comprobó cada vídeo. Las fuentes de cada vídeo figuran en su página. Para pedir una corrección o una actualización, o para oponerse, escriba al contacto indicado arriba. Respondemos en el plazo de un mes.</p>
<h2>Visitantes de estas páginas</h2>
<p>Estas páginas públicas no usan analítica, ni scripts de terceros, ni cookies. El servidor web puede guardar registros técnicos, con direcciones IP, para funcionar y protegerse.</p>
<h2>Cuentas de Google y YouTube</h2>
<p>La aplicación «8bitnews publisher» usa los servicios de la API de YouTube para subir vídeos a los canales de YouTube del propio titular, gestionar esos vídeos y leer las estadísticas de esos canales. Solo trabaja con los canales que el titular ha conectado y no guarda nada sobre espectadores concretos ni sobre otras cuentas. Guarda el token que emite Google, cifrado, con el identificador y el nombre del canal. No se comparten con nadie y se borran cuando el titular desconecta el canal. El acceso también se puede retirar en la <a href="https://myaccount.google.com/permissions">página de permisos de Google</a>. Véanse también las <a href="https://www.youtube.com/t/terms">Condiciones de YouTube</a> y la <a href="https://policies.google.com/privacy">Política de privacidad de Google</a>.</p>
</div>
</main></body></html>
`))
