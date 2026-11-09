package admin

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	loginTTL   = 10 * time.Minute
	sessionTTL = 30 * 24 * time.Hour

	kindLogin   = "login"
	kindSession = "session"
)

var errBadToken = errors.New("bad or expired token")

// Identity is who a token speaks for: a Telegram user, signed the way the
// moderation bot signs decisions.
type Identity struct {
	UserID int64
	Name   string
	// Epoch is the number the session was issued under: see Store.
	Epoch int
}

type claims struct {
	Kind   string `json:"k"`
	UserID int64  `json:"u"`
	Name   string `json:"n"`
	Expiry int64  `json:"e"`
	Nonce  string `json:"j,omitempty"`
	Epoch  int    `json:"v,omitempty"`
	// Channel: what a connect to YouTube is for (youtube_connect.go).
	Channel string `json:"c,omitempty"`
}

// Auth issues and checks the signed tokens behind the admin page. There are
// no passwords: the moderation bot hands a one-time login link to a moderator
// who asks for it in a private chat, and the link becomes a session cookie.
type Auth struct {
	key []byte
	now func() time.Time

	mu   sync.Mutex
	used map[string]time.Time // nonces of login links already spent, until they expire anyway
}

// NewAuth derives the signing key from secret (the bot's token, which already
// decides who moderates).
func NewAuth(secret string) *Auth {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("8bitnews hub admin v1"))
	return &Auth{key: mac.Sum(nil), now: time.Now, used: map[string]time.Time{}}
}

func (a *Auth) sign(c claims) string {
	payload, _ := json.Marshal(c) // claims holds only strings and numbers
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(a.mac(body))
}

func (a *Auth) mac(body string) []byte {
	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

func (a *Auth) verify(token, kind string) (claims, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return claims{}, errBadToken
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.mac(body)) {
		return claims{}, errBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return claims{}, errBadToken
	}
	var c claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return claims{}, errBadToken
	}
	if c.Kind != kind || c.UserID == 0 || a.now().Unix() >= c.Expiry {
		return claims{}, errBadToken
	}
	return c, nil
}

// LoginToken is what goes into a login link: good for loginTTL, once.
func (a *Auth) LoginToken(id Identity) string {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		panic(err) // crypto/rand doesn't fail on a supported platform
	}
	return a.sign(claims{Kind: kindLogin, UserID: id.UserID, Name: id.Name,
		Expiry: a.now().Add(loginTTL).Unix(), Nonce: hex.EncodeToString(nonce[:])})
}

// PeekLogin checks a login token without spending it: opening the link only
// shows a button, so a link preview or a security scanner can't use it up.
func (a *Auth) PeekLogin(token string) (Identity, error) {
	c, err := a.verify(token, kindLogin)
	if err != nil {
		return Identity{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, spent := a.used[c.Nonce]; spent {
		return Identity{}, errBadToken
	}
	return Identity{UserID: c.UserID, Name: c.Name}, nil
}

// SpendLogin checks a login token and uses it up. Spent nonces live in memory:
// a restart forgets them, and a link is good for only loginTTL anyway.
func (a *Auth) SpendLogin(token string) (Identity, error) {
	c, err := a.verify(token, kindLogin)
	if err != nil {
		return Identity{}, err
	}
	now := a.now()
	a.mu.Lock()
	defer a.mu.Unlock()
	for nonce, expires := range a.used {
		if now.After(expires) {
			delete(a.used, nonce)
		}
	}
	if _, spent := a.used[c.Nonce]; spent {
		return Identity{}, errBadToken
	}
	a.used[c.Nonce] = time.Unix(c.Expiry, 0)
	return Identity{UserID: c.UserID, Name: c.Name}, nil
}

// Session is the cookie's value after a login.
func (a *Auth) Session(id Identity) string {
	return a.sign(claims{Kind: kindSession, UserID: id.UserID, Name: id.Name, Epoch: id.Epoch, Expiry: a.now().Add(sessionTTL).Unix()})
}

// CheckSession reads a session cookie's value back.
func (a *Auth) CheckSession(token string) (Identity, error) {
	c, err := a.verify(token, kindSession)
	if err != nil {
		return Identity{}, err
	}
	return Identity{UserID: c.UserID, Name: c.Name, Epoch: c.Epoch}, nil
}
