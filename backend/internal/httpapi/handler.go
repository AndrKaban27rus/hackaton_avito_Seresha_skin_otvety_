package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/example/ai-buddy/backend/internal/app"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	app *app.Service
	log *slog.Logger
}

func New(a *app.Service, l *slog.Logger) http.Handler {
	h := &Handler{app: a, log: l}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/v1/onboarding", h.steps)
	mux.HandleFunc("POST /api/v1/chat", h.chat)
	mux.HandleFunc("POST /api/v1/steps/{id}/complete", h.complete)
	return cors(mux)
}
func (h *Handler) steps(w http.ResponseWriter, r *http.Request) {
	x, e := h.app.Steps(r.Context())
	if e != nil {
		fail(w, 500, "could not load onboarding")
		return
	}
	write(w, 200, map[string]any{"steps": x})
}
func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req) != nil {
		fail(w, 400, "invalid request")
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" || len([]rune(req.Message)) > 2000 {
		fail(w, 400, "message must be 1-2000 characters")
		return
	}
	a, e := h.app.Ask(r.Context(), req.Message)
	if e != nil {
		h.log.Error("assistant failed", "error", e)
		fail(w, 502, "assistant temporarily unavailable")
		return
	}
	write(w, 200, a)
}
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	x, e := h.app.Complete(r.Context(), id)
	if errors.Is(e, pgx.ErrNoRows) {
		fail(w, 404, "step not found")
		return
	}
	if e != nil {
		fail(w, 500, "could not complete step")
		return
	}
	write(w, 200, x)
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"error": msg})
}
