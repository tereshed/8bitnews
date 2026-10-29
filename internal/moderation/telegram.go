package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Telegram is the slice of the Bot API (https://core.telegram.org/bots/api)
// the moderation bot uses. Every call returns the API's own error text.
type Telegram struct {
	base   string
	token  string
	client *http.Client
}

// NewTelegram builds a client for the bot with token, against base
// ("https://api.telegram.org", or a test server).
func NewTelegram(base, token string) *Telegram {
	// Long polling holds a request for up to pollTimeout; uploads take longer.
	return &Telegram{base: base, token: token, client: &http.Client{Timeout: 5 * time.Minute}}
}

// User, Chat, Message, CallbackQuery, Update and ChatMember mirror the Bot
// API's objects, with only the fields the bot reads.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type Message struct {
	MessageID int64    `json:"message_id"`
	From      *User    `json:"from"`
	Chat      Chat     `json:"chat"`
	Text      string   `json:"text"`
	Caption   string   `json:"caption"`
	ReplyTo   *Message `json:"reply_to_message"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

// Button is one inline keyboard button; Keyboard is rows of them.
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type Keyboard struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}

// forceReply asks the client to open a reply to the bot's message.
type forceReply struct {
	ForceReply            bool   `json:"force_reply"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

// errBadURL: the request URL didn't parse. It carries the token, so the
// error doesn't quote it (a stray character in the token or the API base).
func errBadURL(method string) error {
	return fmt.Errorf("telegram %s: the API URL doesn't parse: check HUB_TELEGRAM_API_BASE and the token", method)
}

func (t *Telegram) url(method string) string {
	return t.base + "/bot" + t.token + "/" + method
}

func (t *Telegram) call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: encoding: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url(method), bytes.NewReader(body))
	if err != nil {
		return errBadURL(method)
	}
	req.Header.Set("Content-Type", "application/json")
	return t.do(req, method, out)
}

func (t *Telegram) do(req *http.Request, method string, out any) error {
	resp, err := t.client.Do(req)
	if err != nil {
		// The URL carries the token: keep only the cause, never the URL.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var r apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, unreadable reply", method, resp.StatusCode)
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %d %s", method, r.ErrorCode, r.Description)
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decoding result: %w", method, err)
		}
	}
	return nil
}

// GetMe returns the bot itself.
func (t *Telegram) GetMe(ctx context.Context) (User, error) {
	var u User
	err := t.call(ctx, "getMe", struct{}{}, &u)
	return u, err
}

// GetUpdates long-polls for messages and button presses after offset.
func (t *Telegram) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	var updates []Update
	err := t.call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(timeout.Seconds()),
		"allowed_updates": []string{"message", "callback_query"},
	}, &updates)
	return updates, err
}

// SendVideo uploads a video to chatID with a caption and buttons.
func (t *Telegram) SendVideo(ctx context.Context, chatID int64, filename string, video io.Reader, caption string, keyboard *Keyboard) (Message, error) {
	fields := map[string]string{
		"chat_id":            strconv.FormatInt(chatID, 10),
		"caption":            caption,
		"supports_streaming": "true",
	}
	if keyboard != nil {
		raw, err := json.Marshal(keyboard)
		if err != nil {
			return Message{}, fmt.Errorf("telegram sendVideo: encoding keyboard: %w", err)
		}
		fields["reply_markup"] = string(raw)
	}
	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			for k, v := range fields {
				if err := form.WriteField(k, v); err != nil {
					return err
				}
			}
			part, err := form.CreateFormFile("video", filename)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, video); err != nil {
				return err
			}
			return form.Close()
		}()
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url("sendVideo"), pr)
	if err != nil {
		pr.CloseWithError(err)
		return Message{}, errBadURL("sendVideo")
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	var m Message
	err = t.do(req, "sendVideo", &m)
	pr.CloseWithError(io.ErrClosedPipe) // unblocks the writer if the request ended early
	return m, err
}

// SendMessage posts text to chatID, as a reply to replyTo when it isn't 0,
// with markup (a *Keyboard, or forceReply) when it isn't nil.
func (t *Telegram) SendMessage(ctx context.Context, chatID int64, text string, replyTo int64, markup any) (Message, error) {
	params := map[string]any{"chat_id": chatID, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}
	if replyTo != 0 {
		params["reply_parameters"] = map[string]any{"message_id": replyTo, "allow_sending_without_reply": true}
	}
	if markup != nil {
		params["reply_markup"] = markup
	}
	var m Message
	err := t.call(ctx, "sendMessage", params, &m)
	return m, err
}

// RemoveKeyboard takes the buttons off a message.
func (t *Telegram) RemoveKeyboard(ctx context.Context, chatID, messageID int64) error {
	return t.call(ctx, "editMessageReplyMarkup", map[string]any{
		"chat_id": chatID, "message_id": messageID, "reply_markup": Keyboard{InlineKeyboard: [][]Button{}},
	}, nil)
}

// AnswerCallback acknowledges a button press, with a toast (or an alert).
func (t *Telegram) AnswerCallback(ctx context.Context, callbackID, text string, alert bool) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID, "text": text, "show_alert": alert,
	}, nil)
}

// ChatAdministrators lists the chat's administrators, its creator included.
func (t *Telegram) ChatAdministrators(ctx context.Context, chatID int64) ([]ChatMember, error) {
	var members []ChatMember
	err := t.call(ctx, "getChatAdministrators", map[string]any{"chat_id": chatID}, &members)
	return members, err
}
