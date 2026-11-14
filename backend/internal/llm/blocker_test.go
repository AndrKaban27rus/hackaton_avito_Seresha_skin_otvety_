package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

var fixes = []domain.Fix{
	{Match: []string{"доступ"}, NeedsHuman: true, SelfStep: "Подготовь таблицу.", Request: "Выдай доступ."},
	{Match: []string{"флаг"}, SelfStep: "Включи флаг."},
}

func TestFixtureAssessBlocker(t *testing.T) {
	ctx := context.Background()
	f := Fixture{}
	if a, _ := f.AssessBlocker(ctx, BlockerCase{Answer: "Флаг не включается", Fixes: fixes}); a.NeedsHuman || a.SelfStep != "Включи флаг." {
		t.Errorf("self help = %+v", a)
	}
	if a, _ := f.AssessBlocker(ctx, BlockerCase{Answer: "Нет доступа", Fixes: fixes}); !a.NeedsHuman || a.Request != "Выдай доступ." {
		t.Errorf("access = %+v", a)
	}
	// The obstacle itself may name the cause.
	if a, _ := f.AssessBlocker(ctx, BlockerCase{Obstacle: "Нет доступа к стенду", Answer: "Да", Fixes: fixes}); !a.NeedsHuman {
		t.Errorf("obstacle = %+v", a)
	}
	a, _ := f.AssessBlocker(ctx, BlockerCase{Answer: "Нужен наставник, флаг сломан", Check: "Проверь всё.", Fixes: fixes})
	if !a.NeedsHuman || !strings.Contains(a.Request, "Нужен наставник") || a.SelfStep != "Включи флаг." {
		t.Errorf("explicit request = %+v", a)
	}
	a, _ = f.AssessBlocker(ctx, BlockerCase{Answer: "Непонятно что", Check: "Проверь всё."})
	if !a.NeedsHuman || a.SelfStep != "Проверь всё." {
		t.Errorf("unknown = %+v", a)
	}
	if a, _ := f.AssessBlocker(ctx, BlockerCase{Answer: "Помоги", Check: "Проверь всё."}); a.SelfStep != "Проверь всё." {
		t.Errorf("fallback step = %+v", a)
	}
}

func TestOpenRouterAssessBlocker(t *testing.T) {
	answer := map[string]any{"needs_human": true, "self_step": "Подожди.", "request": "Выдай доступ."}
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		user := p["messages"].([]any)[1].(map[string]any)["content"].(string)
		if !strings.Contains(user, `"obstacle":"Не могу"`) || !strings.Contains(user, `"needs_human":true`) {
			t.Errorf("context = %s", user)
		}
		return 200, toolReply("assess_blocker", answer)
	})
	c := BlockerCase{Assignment: Assignment{TicketID: "BACK-112"}, Obstacle: "Не могу", Answer: "Нет доступа", Fixes: fixes}
	got, err := o.AssessBlocker(context.Background(), c)
	if err != nil || !got.NeedsHuman || got.Request != "Выдай доступ." {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range []map[string]any{
		{"needs_human": true, "self_step": "Подожди.", "request": " "},
		{"needs_human": false, "self_step": "", "request": ""},
		{"needs_human": "yes", "self_step": "x", "request": "y"},
	} {
		answer = bad
		if _, err := o.AssessBlocker(context.Background(), c); !errors.Is(err, ErrModel) {
			t.Errorf("%v must be rejected", bad)
		}
	}
	failing := fakeRouter(t, func(map[string]any) (int, any) { return 500, "" })
	if _, err := failing.AssessBlocker(context.Background(), c); !errors.Is(err, ErrModel) {
		t.Error("provider failure")
	}
}

func TestOpenRouterClassifiesBlocker(t *testing.T) {
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		return 200, toolReply("classify_request", map[string]string{"intent": "blocker"})
	})
	if got, err := o.Classify(context.Background(), "Не могу запустить", true, nil); err != nil || got != IntentBlocker {
		t.Fatalf("%s %v", got, err)
	}
}

func TestOpenRouterErrorKinds(t *testing.T) {
	for status, want := range map[int]error{401: ErrModelAuth, 403: ErrModelAuth, 402: ErrModelAuth, 429: ErrModelBusy} {
		o := fakeRouter(t, func(map[string]any) (int, any) { return status, map[string]string{"error": "x"} })
		if _, err := o.Classify(context.Background(), "x", false, nil); !errors.Is(err, want) || !errors.Is(err, ErrModel) {
			t.Errorf("%d: %v", status, err)
		}
	}
	slow := fakeRouter(t, func(map[string]any) (int, any) {
		time.Sleep(200 * time.Millisecond)
		return 200, toolReply("classify_request", map[string]string{"intent": "other"})
	})
	slow.Client.Timeout = 50 * time.Millisecond
	if _, err := slow.Classify(context.Background(), "x", false, nil); !errors.Is(err, ErrModelTimeout) {
		t.Errorf("timeout: %v", err)
	}
}
