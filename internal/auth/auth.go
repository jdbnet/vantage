package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const CookieName = "vantage_session"

type Jar struct {
	secret []byte
	ttl    time.Duration
	secure bool
}

type payload struct {
	User string `json:"u"`
	Exp  int64  `json:"e"`
}

func NewJar(secret []byte, ttl time.Duration, secure bool) *Jar {
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Jar{secret: secret, ttl: ttl, secure: secure}
}

func (j *Jar) Issue(w http.ResponseWriter, user string) error {
	p := payload{User: user, Exp: time.Now().Add(j.ttl).Unix()}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, j.secret)
	mac.Write(raw)
	tok := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   j.secure,
		MaxAge:   int(j.ttl.Seconds()),
	})
	return nil
}

func (j *Jar) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
	})
}

func (j *Jar) User(r *http.Request) (string, error) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return "", err
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return "", errors.New("bad session")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", err
	}
	sum, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, j.secret)
	mac.Write(raw)
	if !hmac.Equal(mac.Sum(nil), sum) {
		return "", errors.New("bad session")
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	if time.Now().Unix() > p.Exp {
		return "", errors.New("expired")
	}
	return p.User, nil
}

func RandomSecret() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}
