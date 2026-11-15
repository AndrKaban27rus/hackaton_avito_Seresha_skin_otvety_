package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/hackandchange/ai-buddy/backend/internal/buddy"
	"github.com/hackandchange/ai-buddy/backend/internal/devdb"
	"github.com/hackandchange/ai-buddy/backend/internal/httpapi"
	"github.com/hackandchange/ai-buddy/backend/internal/llm"
	"github.com/hackandchange/ai-buddy/backend/internal/seed"
	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func runScheduler(ctx context.Context, app *buddy.Service, log *slog.Logger, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if sent, err := app.RunReminders(ctx); err != nil && ctx.Err() == nil {
			log.Warn("reminder check failed", "error", err)
		} else if sent > 0 {
			log.Info("reminders sent", "count", sent)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dir := os.Getenv("LOCAL_PG_DIR")
		if dir == "" {
			var err error
			if dir, err = devdb.DefaultDir(); err != nil {
				return err
			}
		}
		port, _ := strconv.Atoi(env("LOCAL_PG_PORT", "54329"))
		log.Info("DATABASE_URL is empty, starting local PostgreSQL", "dir", dir, "port", port)
		pg, err := devdb.Start(dir, uint32(port), os.Stderr)
		if err != nil {
			return err
		}
		defer pg.Stop()
		dbURL = pg.URL
	}

	data, err := seed.Load()
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx, data); err != nil {
		return err
	}

	var model llm.Model = llm.Fixture{}
	if key := os.Getenv("LLM_API_KEY"); key != "" {
		router := llm.NewOpenRouter(os.Getenv("LLM_BASE_URL"), key, os.Getenv("LLM_MODEL"))
		router.Log = log
		model = router
		log.Info("model list", "models", router.Models)
	}
	log.Info("model mode", "mode", model.Mode())

	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		if _, err := os.Stat(filepath.Join("..", "frontend", "dist", "index.html")); err == nil {
			staticDir = filepath.Join("..", "frontend", "dist")
		}
	}
	app := buddy.New(db, model, data)
	// Reminders are produced by the server, so they reach the intern even with the chat closed.
	go runScheduler(ctx, app, log, 30*time.Second)

	addr := net.JoinHostPort(env("HOST", "127.0.0.1"), env("PORT", "8080"))
	server := &http.Server{Addr: addr, Handler: httpapi.New(app, log, staticDir),
		ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Info("AI-buddy listening", "url", "http://"+addr, "ui", staticDir != "")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
