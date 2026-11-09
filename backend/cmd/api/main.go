package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/example/ai-buddy/backend/internal/app"
	"github.com/example/ai-buddy/backend/internal/cache"
	"github.com/example/ai-buddy/backend/internal/config"
	"github.com/example/ai-buddy/backend/internal/events"
	"github.com/example/ai-buddy/backend/internal/httpapi"
	"github.com/example/ai-buddy/backend/internal/llm"
	"github.com/example/ai-buddy/backend/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}
	producer := events.NewKafka(cfg.KafkaBrokers, cfg.KafkaTopic, logger)
	defer producer.Close()
	redis := cache.New(cfg.RedisAddr)
	defer redis.Close()
	if err := redis.Ping(ctx); err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	service := app.New(db, llm.NewMock(), producer, redis, logger)
	handler := httpapi.New(service, logger)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("api listening", "port", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
