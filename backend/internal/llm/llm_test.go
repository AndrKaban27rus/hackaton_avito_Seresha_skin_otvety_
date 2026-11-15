package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

func TestFixture(t *testing.T) {
	ctx := context.Background()
	f := Fixture{}
	if f.Mode() != ModeFixture {
		t.Fatal("mode")
	}
	for msg, want := range map[string]Intent{"Подбери задачу": IntentPickTask, "Привет": IntentOther} {
		if got, _ := f.Classify(ctx, msg, false, nil, nil); got.Intent != want {
			t.Errorf("%q -> %+v", msg, got)
		}
	}
	if got, _ := f.Classify(ctx, "С чего начать?", true, nil, nil); got.Intent != IntentTaskQuestion {
		t.Error("question with assignment")
	}
	sel, err := f.Select(ctx, "", []domain.Ticket{{ID: "A", OKRPriority: 1}, {ID: "B", OKRPriority: 2, DemoFirstStep: "s"}}, domain.Profile{}, "")
	if err != nil || sel.TicketID != "B" || sel.FirstStep != "s" {
		t.Errorf("select = %+v %v", sel, err)
	}
	if _, err := f.Select(ctx, "", nil, domain.Profile{}, ""); !errors.Is(err, ErrModel) {
		t.Error("empty candidates fail")
	}
	if a, _ := f.Answer(ctx, "", Assignment{FirstStep: "go", TicketID: "T-1"}, nil); !strings.Contains(a, "Первый шаг: go") ||
		!strings.Contains(a, "нет доступной тебе инструкции по T-1") {
		t.Errorf("answer without instruction = %q", a)
	}
	guide := Assignment{FirstStep: "go", Instruction: "Инструкция", Guide: []string{"Открой дашборд", "Сделай замер"}}
	if a, _ := f.Answer(ctx, "С чего начать?", guide, nil); !strings.Contains(a, "«Инструкция» (тестовый ресурс):\n1. Открой дашборд\n2. Сделай замер") {
		t.Errorf("guide = %q", a)
	}
}

// fakeRouter answers like OpenRouter with the given status and body.
func fakeRouter(t *testing.T, handle func(payload map[string]any) (int, any)) *OpenRouter {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		status, body := handle(payload)
		w.WriteHeader(status)
		if s, ok := body.(string); ok {
			_, _ = io.WriteString(w, s)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return NewOpenRouter(srv.URL+"/", "key", "test-model")
}

func toolReply(name string, args any) map[string]any {
	raw, _ := json.Marshal(args)
	return map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"tool_calls": []any{map[string]any{"type": "function", "function": map[string]any{"name": name, "arguments": string(raw)}}},
	}}}}
}

var candidates = []domain.Ticket{
	{ID: "BACK-112", Title: "Кэш", Description: "p95"},
	{ID: "BACK-118", Title: "Тесты", Description: "фильтры"},
}

func TestOpenRouterClassify(t *testing.T) {
	var seen map[string]any
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		seen = p
		return 200, toolReply("classify_request", map[string]string{"intent": "pick_task"})
	})
	got, err := o.Classify(context.Background(), "Есть что-нибудь?", false, nil, []Turn{{"Привет", "Здравствуй"}})
	if err != nil || got.Intent != IntentPickTask {
		t.Fatalf("%+v %v", got, err)
	}
	if seen["model"] != "test-model" || seen["models"] != nil {
		t.Error("model name is sent")
	}
	msgs := seen["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].(string)
	if !strings.Contains(user, `"previous_turns":[{"assistant":"Здравствуй","user":"Привет"}]`) {
		t.Errorf("context = %s", user)
	}
}

func TestOpenRouterRejectsInvalidAnswers(t *testing.T) {
	cases := map[string]func(map[string]any) (int, any){
		"unknown intent": func(map[string]any) (int, any) {
			return 200, toolReply("classify_request", map[string]string{"intent": "assign_any_ticket"})
		},
		"extra field": func(map[string]any) (int, any) {
			return 200, toolReply("classify_request", map[string]string{"intent": "other", "x": "y"})
		},
		"wrong field": func(map[string]any) (int, any) {
			return 200, toolReply("classify_request", map[string]string{"goal": "other"})
		},
		"wrong type": func(map[string]any) (int, any) {
			return 200, toolReply("classify_request", map[string]int{"intent": 1})
		},
		"wrong tool": func(map[string]any) (int, any) {
			return 200, toolReply("select_ticket", map[string]string{"intent": "other"})
		},
		"no tool": func(map[string]any) (int, any) {
			return 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{}}}}
		},
		"no choices":      func(map[string]any) (int, any) { return 200, map[string]any{"choices": []any{}} },
		"malformed":       func(map[string]any) (int, any) { return 200, "not json" },
		"unauthorized":    func(map[string]any) (int, any) { return 401, map[string]string{"error": "bad key"} },
		"rate limited":    func(map[string]any) (int, any) { return 429, map[string]string{"error": "slow down"} },
		"provider failed": func(map[string]any) (int, any) { return 502, "" },
	}
	for name, handle := range cases {
		o := fakeRouter(t, handle)
		if _, err := o.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModel) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestOpenRouterNetworkFailureAndTimeout(t *testing.T) {
	o := NewOpenRouter("http://127.0.0.1:1", "key", "")
	if _, err := o.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModel) {
		t.Error("network failure is a model error")
	}
	slow := fakeRouter(t, func(map[string]any) (int, any) {
		time.Sleep(200 * time.Millisecond)
		return 200, toolReply("classify_request", map[string]string{"intent": "other"})
	})
	slow.Client.Timeout = 50 * time.Millisecond
	if _, err := slow.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModel) {
		t.Error("timeout is a model error")
	}
	bad := NewOpenRouter("http://[::1", "key", "")
	if _, err := bad.Classify(context.Background(), "x", false, nil, nil); !errors.Is(err, ErrModel) {
		t.Error("bad URL is a model error")
	}
}

func TestOpenRouterSelectSendsOnlyCandidates(t *testing.T) {
	answer := map[string]string{"ticket_id": "BACK-118", "rationale": " Подходит. ", "first_step": "Открой тесты."}
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		user := p["messages"].([]any)[1].(map[string]any)["content"].(string)
		if strings.Contains(user, "Алексей") || strings.Contains(user, "BACK-104") {
			t.Errorf("leaked context: %s", user)
		}
		if !strings.Contains(user, "BACK-112") || !strings.Contains(user, `"grade":"E1"`) {
			t.Errorf("missing context: %s", user)
		}
		return 200, toolReply("select_ticket", answer)
	})
	p := domain.Profile{Name: "Алексей", Grade: "E1", Team: "Поиск"}
	sel, err := o.Select(context.Background(), "Задачу", candidates, p, "Цель")
	if err != nil || sel.TicketID != "BACK-118" || sel.Rationale != "Подходит." {
		t.Fatalf("%+v %v", sel, err)
	}
	for _, bad := range []map[string]string{
		{"ticket_id": "BACK-104", "rationale": "x", "first_step": "y"},
		{"ticket_id": "BACK-112", "rationale": " ", "first_step": "y"},
		{"ticket_id": "BACK-112", "rationale": "x", "first_step": strings.Repeat("я", 601)},
	} {
		answer = bad
		if _, err := o.Select(context.Background(), "Задачу", candidates, p, "Цель"); !errors.Is(err, ErrModel) {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

func TestOpenRouterAnswer(t *testing.T) {
	content := "Открой дашборд."
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		msgs := p["messages"].([]any)
		if len(msgs) < 2 || !strings.Contains(msgs[0].(map[string]any)["content"].(string), "BACK-112") {
			t.Errorf("messages = %v", msgs)
		}
		return 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}}
	})
	a := Assignment{TicketID: "BACK-112", Title: "Кэш", FirstStep: "Замер"}
	got, err := o.Answer(context.Background(), "Что дальше?", a, []Turn{{"С чего начать?", "Замер"}})
	if err != nil || got != content {
		t.Fatalf("%q %v", got, err)
	}
	content = "  "
	if _, err := o.Answer(context.Background(), "?", a, nil); !errors.Is(err, ErrModel) {
		t.Error("empty reply is rejected")
	}
	if o.Mode() != ModeOpenRouter || len(NewOpenRouter("", "k", "").Models) != 2 {
		t.Error("defaults")
	}
}

func TestFixtureExplainsProgressWithoutChangingIt(t *testing.T) {
	a := Assignment{TicketID: "BACK-112", FirstStep: "Замер", Status: "Назначена", Due: "19 ноября, чт",
		NextStep: "Замер", Action: "Взять в работу"}
	got, _ := Fixture{}.Answer(context.Background(), "Как перевести в работу?", a, nil)
	if !strings.Contains(got, "статус «Назначена», срок — 19 ноября, чт") || !strings.Contains(got, "кнопкой «Взять в работу»") {
		t.Errorf("answer = %q", got)
	}
	a.Action = ""
	if strings.Contains(ProgressAnswer(a), "кнопкой") {
		t.Error("no button after review")
	}
}

func TestOpenRouterAnswerGetsStatusContext(t *testing.T) {
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		system := p["messages"].([]any)[0].(map[string]any)["content"].(string)
		if !strings.Contains(system, `"status":"В работе"`) || !strings.Contains(system, "не пиши, что статус уже изменён") || !strings.Contains(system, "во втором лице") {
			t.Errorf("system = %s", system)
		}
		return 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}}
	})
	if _, err := o.Answer(context.Background(), "?", Assignment{Status: "В работе"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRouterAnswerDropsBoldMarkers(t *testing.T) {
	o := fakeRouter(t, func(map[string]any) (int, any) {
		return 200, map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"content": "Открой панель **«p95 latency, ms»** и `filters-smoke-200`."}}}}
	})
	got, err := o.Answer(context.Background(), "?", Assignment{}, nil)
	if err != nil || got != "Открой панель «p95 latency, ms» и `filters-smoke-200`." {
		t.Fatalf("%q %v", got, err)
	}
}

func TestOpenRouterSendsFallbackModels(t *testing.T) {
	var seen map[string]any
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		seen = p
		return 200, toolReply("classify_request", map[string]string{"intent": "other"})
	})
	o.Models = NewOpenRouter("", "k", " first:free , second:free ,").Models
	if _, err := o.Classify(context.Background(), "x", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	models, _ := seen["models"].([]any)
	if seen["model"] != "first:free" || len(models) != 2 || models[1] != "second:free" {
		t.Errorf("payload model=%v models=%v", seen["model"], seen["models"])
	}
}

var faqBase = []domain.FAQ{
	{ID: "vacation", Question: "Как взять отгул?", Answer: "Согласуй с руководителем.", Match: []string{"отгул"}},
	{ID: "code_review", Question: "Как оформить pull request?", Answer: "Ветка от master.", Match: []string{"pull request", "ревьюер"}},
}

func TestFixtureClassifiesFAQ(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		msg  string
		has  bool
		want Classification
	}{
		{"Как взять отгул?", false, Classification{Intent: IntentFAQ, FAQID: "vacation"}},
		{"Кого назначить ревьюером?", true, Classification{Intent: IntentFAQ, FAQID: "code_review"}},
		{"Как отправить задачу на ревью?", true, Classification{Intent: IntentTaskQuestion}},
		{"Не могу открыть pull request", true, Classification{Intent: IntentBlocker}},
		{"Не могу открыть pull request", false, Classification{Intent: IntentFAQ, FAQID: "code_review"}},
		{"Дай задачу, а потом расскажи про отгул", false, Classification{Intent: IntentPickTask}},
		{"Привет", false, Classification{Intent: IntentOther}},
	} {
		if got, _ := (Fixture{}).Classify(ctx, c.msg, c.has, faqBase, nil); got != c.want {
			t.Errorf("%q (assignment %v) -> %+v", c.msg, c.has, got)
		}
	}
}

func TestOpenRouterClassifiesFAQ(t *testing.T) {
	answer := map[string]string{"intent": "faq", "faq_id": "vacation"}
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		msgs := p["messages"].([]any)
		system := msgs[0].(map[string]any)["content"].(string)
		user := msgs[1].(map[string]any)["content"].(string)
		if !strings.Contains(user, `"id":"vacation","keywords":["отгул"],"question":"Как взять отгул?"`) ||
			strings.Contains(user, "Согласуй") {
			t.Errorf("questions and keywords go to the model, answers do not: %s", user)
		}
		if !strings.Contains(system, "faq_id = none") {
			t.Errorf("system = %s", system)
		}
		fn := p["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		props := fn["parameters"].(map[string]any)["properties"].(map[string]any)
		if ids := props["faq_id"].(map[string]any)["enum"].([]any); len(ids) != 3 || ids[2] != "none" {
			t.Errorf("faq ids = %v", ids)
		}
		return 200, toolReply("classify_request", answer)
	})
	ctx := context.Background()
	if got, err := o.Classify(ctx, "Можно отгул?", false, faqBase, nil); err != nil || got != (Classification{Intent: IntentFAQ, FAQID: "vacation"}) {
		t.Fatalf("%+v %v", got, err)
	}
	answer = map[string]string{"intent": "faq", "faq_id": "none"}
	if got, err := o.Classify(ctx, "Какой дресс-код?", false, faqBase, nil); err != nil || got != (Classification{Intent: IntentFAQ}) {
		t.Errorf("no entry fits: %+v %v", got, err)
	}
	answer = map[string]string{"intent": "task_question", "faq_id": "vacation"}
	if got, err := o.Classify(ctx, "С чего начать?", true, faqBase, nil); err != nil || got != (Classification{Intent: IntentTaskQuestion}) {
		t.Errorf("faq_id is dropped for other intents: %+v %v", got, err)
	}
	for _, bad := range []map[string]string{{"intent": "faq", "faq_id": "salary"}, {"intent": "faq"}} {
		answer = bad
		if _, err := o.Classify(ctx, "x", false, faqBase, nil); !errors.Is(err, ErrModel) {
			t.Errorf("%v must be rejected", bad)
		}
	}
}

func TestOpenRouterClassifyWithoutFAQKeepsOneField(t *testing.T) {
	o := fakeRouter(t, func(p map[string]any) (int, any) {
		fn := p["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		props := fn["parameters"].(map[string]any)["properties"].(map[string]any)
		if _, ok := props["faq_id"]; ok || len(props) != 1 {
			t.Errorf("properties = %v", props)
		}
		return 200, toolReply("classify_request", map[string]string{"intent": "faq"})
	})
	if _, err := o.Classify(context.Background(), "Как взять отгул?", false, nil, nil); !errors.Is(err, ErrModel) {
		t.Error("faq is not a valid intent without the base")
	}
}
