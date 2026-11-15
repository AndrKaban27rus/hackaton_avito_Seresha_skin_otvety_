package llm

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

func TestForeignWords(t *testing.T) {
	input := "Флаг search.filters.cache, набор filters-smoke-200, панель p95 latency, ms. BACK-112"
	got := foreignWords("BACK-112 напрямую serviert цели. Включи search.filters.cache, смотри p95 в Grafana и Jira, ms, OK.", input)
	if len(got) != 1 || got[0] != "serviert" {
		t.Errorf("foreign = %v", got)
	}
	if got := foreignWords("Всё по-русски, без латиницы.", ""); len(got) != 0 {
		t.Errorf("foreign = %v", got)
	}
	if got := foreignWords("deploy и Deploy", ""); len(got) != 1 {
		t.Errorf("each word is reported once: %v", got)
	}
}

// sequenceRouter answers each call with the next prepared reply.
func sequenceRouter(t *testing.T, replies ...any) (*OpenRouter, *int, *bytes.Buffer) {
	t.Helper()
	calls := 0
	o := fakeRouter(t, func(map[string]any) (int, any) {
		r := replies[min(calls, len(replies)-1)]
		calls++
		return 200, r
	})
	var logs bytes.Buffer
	o.Log = slog.New(slog.NewTextHandler(&logs, nil))
	return o, &calls, &logs
}

func contentReply(text string) map[string]any {
	return map[string]any{"model": "test/model", "choices": []any{map[string]any{
		"message": map[string]any{"content": text}, "finish_reason": "stop"}}}
}

func TestAnswerRetriesOnceOnForeignWords(t *testing.T) {
	a := Assignment{TicketID: "BACK-112", Title: "Кэш"}
	o, calls, logs := sequenceRouter(t, contentReply("Задача напрямую serviert цели."), contentReply("Задача служит цели."))
	got, err := o.Answer(context.Background(), "Зачем?", a, nil)
	if err != nil || got != "Задача служит цели." || *calls != 2 {
		t.Fatalf("%q %v calls=%d", got, err, *calls)
	}
	if !strings.Contains(logs.String(), "asking again") || !strings.Contains(logs.String(), "model=test/model") {
		t.Errorf("logs = %s", logs.String())
	}

	// Two bad answers in a row: the second is kept, there is no third call.
	o, calls, logs = sequenceRouter(t, contentReply("Erste serviert."), contentReply("Zweite serviert."))
	got, err = o.Answer(context.Background(), "Зачем?", a, nil)
	if err != nil || got != "Zweite serviert." || *calls != 2 || !strings.Contains(logs.String(), "still has foreign words") {
		t.Errorf("%q %v calls=%d", got, err, *calls)
	}

	// A clean answer needs one call.
	o, calls, _ = sequenceRouter(t, contentReply("Открой панель p95."))
	if _, err := o.Answer(context.Background(), "p95?", a, nil); err != nil || *calls != 1 {
		t.Errorf("calls=%d err=%v", *calls, err)
	}
}

func TestSelectAndAssessRetryOnForeignWords(t *testing.T) {
	bad := toolReply("select_ticket", map[string]string{"ticket_id": "BACK-112", "rationale": "Это serviert цели.", "first_step": "Замер."})
	good := toolReply("select_ticket", map[string]string{"ticket_id": "BACK-112", "rationale": "Это служит цели.", "first_step": "Замер."})
	o, calls, _ := sequenceRouter(t, bad, good)
	sel, err := o.Select(context.Background(), "Задачу", candidates, domain.Profile{Grade: "E1"}, "Цель")
	if err != nil || sel.Rationale != "Это служит цели." || *calls != 2 {
		t.Fatalf("%+v %v calls=%d", sel, err, *calls)
	}

	badA := toolReply("assess_blocker", map[string]any{"needs_human": true, "self_step": "Warte bitte.", "request": "Выдай доступ."})
	goodA := toolReply("assess_blocker", map[string]any{"needs_human": true, "self_step": "Подожди.", "request": "Выдай доступ."})
	o, calls, _ = sequenceRouter(t, badA, goodA)
	as, err := o.AssessBlocker(context.Background(), BlockerCase{Obstacle: "Не могу", Answer: "Нет доступа"})
	if err != nil || as.SelfStep != "Подожди." || *calls != 2 {
		t.Fatalf("%+v %v calls=%d", as, err, *calls)
	}
}

func TestProviderFailureIsRetriedOnce(t *testing.T) {
	calls := 0
	o := fakeRouter(t, func(map[string]any) (int, any) {
		calls++
		if calls == 1 {
			return 504, map[string]string{"error": "gateway timeout"}
		}
		return 200, toolReply("classify_request", map[string]string{"intent": "other"})
	})
	if got, err := o.Classify(context.Background(), "x", false, nil, nil); err != nil || got.Intent != IntentOther || calls != 2 {
		t.Fatalf("%+v %v calls=%d", got, err, calls)
	}

	calls = 0
	down := fakeRouter(t, func(map[string]any) (int, any) {
		calls++
		return 503, ""
	})
	if _, err := down.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModelUnavailable) || calls != 2 {
		t.Errorf("err=%v calls=%d", err, calls)
	}

	calls = 0
	limited := fakeRouter(t, func(map[string]any) (int, any) {
		calls++
		return 429, ""
	})
	if _, err := limited.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModelBusy) || calls != 1 {
		t.Errorf("rate limits are not repeated: err=%v calls=%d", err, calls)
	}
}
