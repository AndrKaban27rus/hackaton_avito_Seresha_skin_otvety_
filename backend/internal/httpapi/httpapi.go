// Package httpapi exposes the demo Slack API and, optionally, the built UI.
package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/hackandchange/ai-buddy/backend/internal/buddy"
	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

const (
	cookieName  = "demo_session"
	maxBodySize = 8192
	unavailable = "Тестовый сервис временно недоступен. Повторите запрос."
)

type Handler struct {
	app *buddy.Service
	log *slog.Logger
}

// New builds the router. staticDir, if not empty, serves the built frontend.
func New(app *buddy.Service, log *slog.Logger, staticDir string) http.Handler {
	h := &Handler{app: app, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"}, "")
	})
	mux.HandleFunc("GET /api/state", h.state)
	mux.HandleFunc("POST /api/session", h.post(h.session))
	mux.HandleFunc("POST /api/chat", h.post(h.withUser(h.chat)))
	mux.HandleFunc("POST /api/chat/reset", h.post(h.withUser(h.resetChat)))
	mux.HandleFunc("POST /api/pick/reset", h.post(h.withUser(h.restartPick)))
	mux.HandleFunc("POST /api/task/status", h.post(h.withUser(h.taskStatus)))
	mux.HandleFunc("POST /api/clock", h.post(h.withUser(h.clock)))
	mux.HandleFunc("POST /api/blocker/close", h.post(h.withUser(h.closeBlocker)))
	mux.HandleFunc("GET /demo/{id}", h.demoPage)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, http.StatusNotFound, "Действие не найдено.")
	})
	if staticDir != "" {
		mux.Handle("/", spa(staticDir))
	}
	return headers(mux)
}

func headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// spa serves files from dir and falls back to index.html.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		files.ServeHTTP(w, r)
	})
}

type body map[string]any

func (b body) str(key string) string {
	s, _ := b[key].(string)
	return s
}

// id reads a positive integer ID sent as a JSON number.
func (b body) id(key string) int64 {
	f, ok := b[key].(float64)
	if !ok || f < 1 || f != float64(int64(f)) {
		return 0
	}
	return int64(f)
}

// post checks origin, content type and size, then decodes a JSON object.
func (h *Handler) post(next func(http.ResponseWriter, *http.Request, body)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
				fail(w, http.StatusForbidden, "Источник запроса не разрешён.")
				return
			}
		}
		if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
			fail(w, http.StatusUnsupportedMediaType, "Ожидается application/json.")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
		if err != nil || len(raw) == 0 || len(raw) > maxBodySize {
			fail(w, http.StatusBadRequest, "Недопустимый размер запроса.")
			return
		}
		var b body
		if json.Unmarshal(raw, &b) != nil || b == nil {
			fail(w, http.StatusBadRequest, "Ожидается JSON-объект.")
			return
		}
		next(w, r, b)
	}
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) user(r *http.Request) (string, error) {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return "", &buddy.Problem{Status: http.StatusUnauthorized, Message: "Выберите тестовый профиль."}
	}
	return h.app.SessionUser(r.Context(), tokenHash(c.Value))
}

func (h *Handler) withUser(next func(http.ResponseWriter, *http.Request, body, string)) func(http.ResponseWriter, *http.Request, body) {
	return func(w http.ResponseWriter, r *http.Request, b body) {
		user, err := h.user(r)
		if err != nil {
			h.error(w, err)
			return
		}
		next(w, r, b, user)
	}
}

func (h *Handler) respondState(w http.ResponseWriter, r *http.Request, user, cookie string) {
	st, err := h.app.State(r.Context(), user)
	if err != nil {
		h.error(w, err)
		return
	}
	write(w, http.StatusOK, st, cookie)
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	user, err := h.user(r)
	if err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request, b body) {
	profile := b.str("profile")
	if profile == "" {
		fail(w, http.StatusBadRequest, "Неизвестный тестовый профиль.")
		return
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := base64.RawURLEncoding.EncodeToString(buf)
	old := ""
	if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
		old = tokenHash(c.Value)
	}
	if err := h.app.Login(r.Context(), profile, tokenHash(token), old); err != nil {
		h.error(w, err)
		return
	}
	cookie := (&http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}).String()
	h.respondState(w, r, profile, cookie)
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request, b body, user string) {
	if err := h.app.Chat(r.Context(), user, b.str("message"), b.str("request_id")); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) resetChat(w http.ResponseWriter, r *http.Request, _ body, user string) {
	if err := h.app.ResetChat(r.Context(), user, false); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) restartPick(w http.ResponseWriter, r *http.Request, _ body, user string) {
	if err := h.app.ResetChat(r.Context(), user, true); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) taskStatus(w http.ResponseWriter, r *http.Request, b body, user string) {
	if err := h.app.Transition(r.Context(), user, b.str("ticket_id"), b.str("status")); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) clock(w http.ResponseWriter, r *http.Request, b body, user string) {
	if err := h.app.Clock(r.Context(), b.str("action")); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

func (h *Handler) closeBlocker(w http.ResponseWriter, r *http.Request, b body, user string) {
	id := b.id("blocker_id")
	if id == 0 {
		fail(w, http.StatusBadRequest, "Некорректный номер блокера.")
		return
	}
	if err := h.app.CloseBlocker(r.Context(), user, id); err != nil {
		h.error(w, err)
		return
	}
	h.respondState(w, r, user, "")
}

var demoPage = template.Must(template.New("demo").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
body{margin:0;font:15px/1.55 "Segoe UI",Arial,sans-serif;color:#232027;background:#fff}
.banner{background:#fff6db;border-bottom:1px solid #f0e2b0;color:#5c4a12;padding:8px 20px;font-weight:600}
main{max-width:760px;padding:20px}
h1{font-size:22px;margin:0 0 4px}.kind{color:#615764;margin:0 0 16px}
li{margin:6px 0}a{color:#1f4f8f}
</style></head><body>
<div class="banner">Тестовый ресурс · синтетические данные · {{.Kind}}</div>
<main><h1>{{.Title}}</h1><p class="kind">Задача {{.TicketID}}. Корпоративные системы не подключены.</p>
{{if .Steps}}<ol>{{range .Steps}}<li>{{.}}</li>{{end}}</ol>{{end}}
{{if .Lines}}<ul>{{range .Lines}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .Check}}<p><strong>Проверка:</strong> {{.Check}}</p>{{end}}
</main></body></html>`))

// demoPage renders a synthetic resource after the same access check as the API.
func (h *Handler) demoPage(w http.ResponseWriter, r *http.Request) {
	user, err := h.user(r)
	if err == nil {
		var doc domain.Doc
		if doc, err = h.app.Resource(r.Context(), user, r.PathValue("id")); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = demoPage.Execute(w, doc)
			return
		}
	}
	status, msg := http.StatusServiceUnavailable, unavailable
	var p *buddy.Problem
	if errors.As(err, &p) {
		status, msg = p.Status, p.Message
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

// error maps service problems to their status; anything else is a storage failure.
func (h *Handler) error(w http.ResponseWriter, err error) {
	var p *buddy.Problem
	if errors.As(err, &p) {
		if p.Cause != nil {
			h.log.Warn("external service failed", "status", p.Status, "error", p.Cause)
		}
		fail(w, p.Status, p.Message)
		return
	}
	h.log.Error("request failed", "error", err)
	fail(w, http.StatusServiceUnavailable, unavailable)
}

func write(w http.ResponseWriter, status int, v any, cookie string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if cookie != "" {
		w.Header().Set("Set-Cookie", cookie)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": strings.TrimSpace(msg)}, "")
}
