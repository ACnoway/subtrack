package app

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/ACnoway/subtrack/internal/store"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Server struct {
	store  *store.Store
	cfg    Config
	logger *slog.Logger
	zone   *time.Location
}

func NewServer(database *store.Store, cfg Config, logger *slog.Logger) *Server {
	return &Server{store: database, cfg: cfg, logger: logger, zone: time.Local}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS()))))
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.Handle("GET /{$}", s.authenticated(http.HandlerFunc(s.home)))
	mux.Handle("POST /logout", s.authenticated(http.HandlerFunc(s.logout)))
	mux.Handle("GET /subscriptions", s.authenticated(http.HandlerFunc(s.listSubscriptions)))
	mux.Handle("GET /subscriptions/new", s.authenticated(http.HandlerFunc(s.newSubscription)))
	mux.Handle("POST /subscriptions", s.authenticated(http.HandlerFunc(s.createSubscription)))
	mux.Handle("GET /subscriptions/{id}/edit", s.authenticated(http.HandlerFunc(s.editSubscription)))
	mux.Handle("POST /subscriptions/{id}", s.authenticated(http.HandlerFunc(s.updateSubscription)))
	mux.Handle("POST /subscriptions/{id}/archive", s.authenticated(http.HandlerFunc(s.archiveSubscription)))
	mux.Handle("POST /subscriptions/{id}/restore", s.authenticated(http.HandlerFunc(s.restoreSubscription)))
	mux.Handle("POST /subscriptions/{id}/delete", s.authenticated(http.HandlerFunc(s.deleteSubscription)))
	return s.recoverPanics(s.logRequests(mux))
}

func staticFS() http.FileSystem {
	files, _ := fs.Sub(assets, "static")
	return http.FS(files)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "error", recovered, "path", r.URL.Path)
				http.Error(w, "服务器内部错误", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
