package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookie = "subtrack_session"
	csrfCookie    = "subtrack_login_csrf"
	sessionLife   = 24 * time.Hour
)

type session struct {
	CSRF string
}

func (s *Server) signSession(csrf string, expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10) + ":" + csrf
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) readSession(r *http.Request) (session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return session{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return session{}, false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return session{}, false
	}
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return session{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return session{}, false
	}
	fields := strings.SplitN(string(payload), ":", 2)
	if len(fields) != 2 {
		return session{}, false
	}
	expires, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || time.Now().Unix() >= expires || fields[1] == "" {
		return session{}, false
	}
	return session{CSRF: fields[1]}, true
}

func (s *Server) setSession(w http.ResponseWriter, csrf string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: s.signSession(csrf, time.Now().Add(sessionLife)),
		Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionLife.Seconds()),
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func (s *Server) setLoginCSRF(w http.ResponseWriter) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: 10 * 60,
	})
	return token, nil
}

func (s *Server) authenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.readSession(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && r.FormValue("csrf_token") != sess.CSRF {
			http.Error(w, "CSRF token 校验失败", http.StatusForbidden)
			return
		}
		ctx := withSession(r, sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type sessionContextKey struct{}

func withSession(ctx context.Context, sess session) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sess)
}

func sessionFromContext(ctx context.Context) session {
	sess, _ := ctx.Value(sessionContextKey{}).(session)
	return sess
}
