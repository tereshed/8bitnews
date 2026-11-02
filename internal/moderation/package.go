package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"path"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

// The publication package (E4, by hand for now): when a release is approved
// the bot posts under its card what the moderator uploads on each platform:
// the title, description and tags to copy, the settings to tick, and the
// files the card doesn't carry (YouTube's cut without music, the cover).
// «📦 Пакет ещё раз» on the card posts it again.

// sendPackage builds and posts the package of an approved release under its
// card; opening is the header's first line.
func (m *Moderator) sendPackage(ctx context.Context, r store.Release, cardID int64, opening string) {
	opening = html.EscapeString(opening)
	pkg, err := m.buildPackage(ctx, r)
	if err != nil {
		m.log.Error("moderation: building the publication package failed", "release_id", r.ID, "error", err)
		m.sendHTML(ctx, r.ID, opening+"\n⚠️ Пакет для публикации не собрался: "+html.EscapeString(err.Error())+
			"\nПопробуйте ещё раз кнопкой «📦 Пакет ещё раз».", cardID)
		return
	}
	var card struct {
		Video string `json:"video"`
	}
	_ = json.Unmarshal(r.Media, &card)

	m.sendHTML(ctx, r.ID, packageHeader(opening, pkg, r.Status == "published"), cardID)
	for _, p := range pkg.Platforms {
		last := cardID
		texts, descriptionAsFile := platformMessages(p, p.Video == card.Video)
		for _, text := range texts {
			if id := m.sendHTML(ctx, r.ID, text, cardID); id != 0 {
				last = id
			}
		}
		if descriptionAsFile {
			m.sendFile(ctx, r.ID, p.Platform+"-description.txt", func() (io.ReadCloser, int64, error) {
				return io.NopCloser(strings.NewReader(p.Description)), int64(len(p.Description)), nil
			}, "📝 "+p.Name+": описание целиком", last)
		}
		if p.Video != card.Video {
			m.sendObject(ctx, r.ID, p.Video, "🎞 "+p.Name+": видео без музыки — загружайте этот файл", last)
		}
		if p.Cover != "" {
			m.sendObject(ctx, r.ID, p.Cover, "🖼 "+p.Name+": обложка", last)
		}
	}
	m.mu.Lock()
	m.packaged[r.ID] = time.Now()
	m.mu.Unlock()
	names := make([]string, len(pkg.Platforms))
	for i, p := range pkg.Platforms {
		names[i] = p.Platform
	}
	m.event(ctx, map[string]any{"release_id": r.ID, "action": "package", "platforms": names, "warnings": pkg.Warnings})
}

func (m *Moderator) buildPackage(ctx context.Context, r store.Release) (publish.Package, error) {
	sources, err := m.store.ListReleaseSources(ctx, r.ID)
	if err != nil {
		return publish.Package{}, err
	}
	ch, err := m.store.GetChannel(ctx, r.ChannelID)
	if err != nil {
		return publish.Package{}, err
	}
	country, err := m.store.GetCountry(ctx, ch.CountryCC)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return publish.Package{}, err
	}
	return publish.Build(publish.Input{Release: r, Sources: sources, Channel: ch, Country: country, Config: m.pub}), nil
}

// sendHTML posts one message of the package, reporting a failure in the
// group in plain text; it returns the message id, or 0.
func (m *Moderator) sendHTML(ctx context.Context, releaseID, text string, replyTo int64) int64 {
	msg, err := m.tg.SendHTML(ctx, m.chatID, text, replyTo)
	if err == nil {
		return msg.MessageID
	}
	m.log.Error("moderation: posting the publication package failed", "release_id", releaseID, "error", err)
	m.reportFailure(ctx, releaseID, "⚠️ Часть пакета для публикации не отправилась: "+err.Error(), replyTo)
	return 0
}

// sendObject posts a file of the release from the object store.
func (m *Moderator) sendObject(ctx context.Context, releaseID, key, caption string, replyTo int64) {
	m.sendFile(ctx, releaseID, path.Base(key), func() (io.ReadCloser, int64, error) { return m.media.Open(ctx, key) }, caption, replyTo)
}

// sendFile posts a file as a document, opening it again for each attempt
// Telegram asks to repeat.
func (m *Moderator) sendFile(ctx context.Context, releaseID, filename string, open func() (io.ReadCloser, int64, error), caption string, replyTo int64) {
	upload := func() error {
		f, size, err := open()
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if size > maxVideoBytes {
			return fmt.Errorf("%s: %d МБ — больше, чем бот может прислать в Telegram (50 МБ)", filename, size>>20)
		}
		_, err = m.tg.SendDocument(ctx, m.chatID, filename, f, caption, replyTo)
		return err
	}
	var err error
	for attempt := 1; ; attempt++ {
		err = upload()
		wait, again := retryWait(err)
		if !again || attempt == callAttempts || !sleep(ctx, wait) {
			break
		}
	}
	if err != nil {
		m.log.Error("moderation: posting a package file failed", "release_id", releaseID, "file", filename, "error", err)
		m.reportFailure(ctx, releaseID, "⚠️ Файл для пакета не отправился: "+err.Error(), replyTo)
	}
}

func (m *Moderator) reportFailure(ctx context.Context, releaseID, text string, replyTo int64) {
	text += "\nОтправить пакет заново — кнопка «📦 Пакет ещё раз» под карточкой."
	if _, err := m.tg.SendMessage(ctx, m.chatID, text, replyTo, nil); err != nil {
		m.log.Error("moderation: reporting the failure failed", "release_id", releaseID, "error", err)
	}
}

// headerWarningsMax keeps the header within messageLimit: markup can't be
// cut safely.
const headerWarningsMax = 10

func packageHeader(opening string, pkg publish.Package, published bool) string {
	var b strings.Builder
	b.WriteString(opening + "\n")
	if len(pkg.Platforms) > 0 {
		names := make([]string, len(pkg.Platforms))
		for i, p := range pkg.Platforms {
			names[i] = p.Name
		}
		b.WriteString("📦 Пакет для публикации — ниже: " + html.EscapeString(strings.Join(names, ", ")) +
			". Выложите ролик вручную и на каждой площадке включите метку AI-контента.\n")
	}
	if pkg.SourcesPage != "" {
		b.WriteString("🔗 Страница источников: " + html.EscapeString(pkg.SourcesPage))
		if published {
			b.WriteString(" — открыта.\n")
		} else {
			b.WriteString(" — откроется, когда нажмёте «🟢 Выложено на площадки» под карточкой.\n")
		}
	}
	for _, n := range pkg.Notes {
		b.WriteString("ℹ️ " + html.EscapeString(truncate(n, 300)) + "\n")
	}
	if len(pkg.Warnings) > 0 {
		b.WriteString("\n⚠️ <b>Сначала поправьте:</b>\n")
		for i, w := range pkg.Warnings {
			if i == headerWarningsMax {
				fmt.Fprintf(&b, "…и ещё %d\n", len(pkg.Warnings)-i)
				break
			}
			b.WriteString("• " + html.EscapeString(truncate(w, 300)) + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// htmlText is a message in Telegram's HTML markup, with its text as
// Telegram counts it against messageLimit (after the markup is parsed).
type htmlText struct {
	markup, plain strings.Builder
}

func (t *htmlText) text(s string) *htmlText {
	t.markup.WriteString(html.EscapeString(s))
	t.plain.WriteString(s)
	return t
}

func (t *htmlText) bold(s string) *htmlText {
	t.markup.WriteString("<b>" + html.EscapeString(s) + "</b>")
	t.plain.WriteString(s)
	return t
}

// field is a label and a block the moderator copies with one tap.
func (t *htmlText) field(label, value string) *htmlText {
	t.bold(label).text("\n")
	t.markup.WriteString("<pre>" + html.EscapeString(value) + "</pre>\n")
	t.plain.WriteString(value + "\n")
	return t
}

func (t *htmlText) size() int { return utf16Len(t.plain.String()) }

// platformMessages is one platform's part of the package: one message, or
// two when the description doesn't fit next to the rest. A description
// longer than a whole message goes as a file (descriptionAsFile).
func platformMessages(p publish.PlatformPackage, videoInCard bool) (texts []string, descriptionAsFile bool) {
	var head, desc, rest htmlText
	head.text("📺 ").bold(p.Name).text("\n")
	if videoInCard {
		head.text("🎞 Видео — то же, что в карточке выше.\n")
	} else {
		head.text("🎞 Видео — файл ниже, версия без музыки (не из карточки).\n")
	}
	if p.Cover != "" {
		head.text("🖼 Обложка — файл ниже.\n")
	}
	head.text("\n")
	if p.Title != "" {
		head.field("Заголовок", p.Title)
	}
	label := "Подпись"
	if p.Title != "" {
		label = "Описание"
	}
	desc.field(label, p.Description)
	if len(p.Tags) > 0 {
		rest.field("Теги", strings.Join(p.Tags, ", "))
	}
	if len(p.Settings) > 0 {
		rest.bold("Настройки").text("\n")
		for _, s := range p.Settings {
			rest.text("• " + s + "\n")
		}
	}
	switch {
	case head.size()+desc.size()+rest.size() <= messageLimit:
		return []string{head.markup.String() + desc.markup.String() + rest.markup.String()}, false
	case desc.size() <= messageLimit:
		return []string{head.markup.String() + rest.markup.String(), desc.markup.String()}, false
	default:
		head.bold(label).text(" — файлом ниже: оно длиннее сообщения Telegram.\n\n")
		return []string{head.markup.String() + rest.markup.String()}, true
	}
}
