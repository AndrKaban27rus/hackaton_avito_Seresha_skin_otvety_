package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/example/ai-buddy/backend/internal/cache"
	"github.com/example/ai-buddy/backend/internal/events"
	"github.com/example/ai-buddy/backend/internal/llm"
	"github.com/example/ai-buddy/backend/internal/store"
)

type Service struct {
	db        *store.Store
	assistant llm.Assistant
	events    events.Publisher
	cache     *cache.Redis
	logger    *slog.Logger
}

func New(db *store.Store, a llm.Assistant, e events.Publisher, c *cache.Redis, l *slog.Logger) *Service {
	return &Service{db: db, assistant: a, events: e, cache: c, logger: l}
}
func (s *Service) Steps(ctx context.Context) ([]store.Step, error) { return s.db.ListSteps(ctx) }
func (s *Service) Ask(ctx context.Context, q string) (llm.Answer, error) {
	// Cache exact demo prompts briefly; production should cache by ACL and corpus version too.
	digest := sha256.Sum256([]byte(q))
	key := "answer:" + hex.EncodeToString(digest[:])
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, key); err == nil {
			var answer llm.Answer
			if json.Unmarshal([]byte(raw), &answer) == nil {
				return answer, nil
			}
		}
	}
	_ = s.db.SaveMessage(ctx, "user", q)
	c, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	a, err := s.assistant.Ask(c, q)
	if err != nil {
		return llm.Answer{}, err
	}
	_ = s.db.SaveMessage(ctx, "assistant", a.Text)
	if s.cache != nil {
		if b, err := json.Marshal(a); err == nil {
			_ = s.cache.Set(ctx, key, string(b), 5*time.Minute)
		}
	}
	return a, nil
}
func (s *Service) Complete(ctx context.Context, id string) (store.Step, error) {
	step, err := s.db.CompleteStep(ctx, id)
	if err != nil {
		return store.Step{}, err
	}
	if s.events != nil {
		if e := s.events.Publish(ctx, id, map[string]any{"type": "onboarding.step.completed", "stepId": id, "at": time.Now().UTC()}); e != nil {
			s.logger.Warn("completion event not delivered", "stepId", id)
		}
	}
	return step, nil
}
