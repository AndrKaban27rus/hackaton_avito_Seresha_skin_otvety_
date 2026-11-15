// Package llm is the model port. Fixture answers without a key; OpenRouter
// calls the real model. Both return only values the caller validates.
package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

type Intent string

const (
	IntentPickTask     Intent = "pick_task"
	IntentTaskQuestion Intent = "task_question"
	IntentOther        Intent = "other"
	IntentBlocker      Intent = "blocker"
	// IntentFAQ is a general question about work in the team, answered from the FAQ base.
	IntentFAQ Intent = "faq"

	ModeFixture    = "fixture"
	ModeOpenRouter = "openrouter"
)

// Classification is the intent of a message. FAQID is set only for IntentFAQ
// and only to an ID from the entries the caller passed; empty means no entry fits.
type Classification struct {
	Intent Intent
	FAQID  string
}

// Turn is one stored question and answer, oldest first.
type Turn struct {
	Question string
	Answer   string
}

// Selection is a model choice that the service still checks against candidates.
type Selection struct {
	TicketID  string
	Rationale string
	FirstStep string
}

// Assignment is the context the model receives for follow-up questions.
type Assignment struct {
	TicketID  string
	Title     string
	Rationale string
	FirstStep string
	// Status, Due and NextStep come from Jira and the backend, never from the model.
	Status   string
	Due      string
	NextStep string
	// Action is the button that changes the status, empty when none is left.
	Action string
	// Instruction is the Confluence instruction the intern may read; empty when none.
	Instruction string
	Guide       []string
}

// BlockerCase is what the model sees when it assesses a clarified blocker.
type BlockerCase struct {
	Assignment Assignment
	Obstacle   string
	Answer     string
	Check      string
	Fixes      []domain.Fix
}

// Assessment decides whether a person must step in.
type Assessment struct {
	NeedsHuman bool
	SelfStep   string
	Request    string
}

// Model chooses intent, picks among permitted tickets and answers about the current one.
// Classify also picks the matching FAQ entry, so a frequent question costs one model call.
type Model interface {
	Mode() string
	Classify(ctx context.Context, message string, hasAssignment bool, faq []domain.FAQ, history []Turn) (Classification, error)
	Select(ctx context.Context, question string, candidates []domain.Ticket, p domain.Profile, goal string) (Selection, error)
	Answer(ctx context.Context, question string, a Assignment, history []Turn) (string, error)
	AssessBlocker(ctx context.Context, c BlockerCase) (Assessment, error)
}

// ErrModel marks any failure of the model call or an invalid model answer.
// The more specific errors below wrap it and change what the user is told.
var (
	ErrModel        = errors.New("model failure")
	ErrModelAuth    = errors.New("model key rejected")
	ErrModelBusy    = errors.New("model rate limited")
	ErrModelTimeout = errors.New("model timed out")
	// ErrModelUnavailable is a 5xx from the provider, such as 504 Gateway Timeout.
	ErrModelUnavailable = errors.New("model provider unavailable")
)

// Fixture is the deterministic demo model.
type Fixture struct{}

func (Fixture) Mode() string { return ModeFixture }

// Classify applies the rules in order: pick a task, a blocker, an FAQ keyword, a task question.
func (Fixture) Classify(_ context.Context, message string, hasAssignment bool, faq []domain.FAQ, _ []Turn) (Classification, error) {
	switch {
	case domain.WantsTicket(message):
		return Classification{Intent: IntentPickTask}, nil
	case hasAssignment && domain.ReportsBlocker(message):
		return Classification{Intent: IntentBlocker}, nil
	}
	if f, ok := domain.MatchFAQ(faq, message); ok {
		return Classification{Intent: IntentFAQ, FAQID: f.ID}, nil
	}
	// Only a message about the task is a task question: «как мне уволиться?» is not.
	if hasAssignment && (domain.AsksAboutTask(message) || domain.AsksAboutProgress(message)) {
		return Classification{Intent: IntentTaskQuestion}, nil
	}
	return Classification{Intent: IntentOther}, nil
}

func (Fixture) Select(_ context.Context, _ string, candidates []domain.Ticket, _ domain.Profile, _ string) (Selection, error) {
	if len(candidates) == 0 {
		return Selection{}, ErrModel
	}
	best := candidates[0]
	for _, t := range candidates[1:] {
		if t.OKRPriority > best.OKRPriority {
			best = t
		}
	}
	return Selection{TicketID: best.ID, Rationale: best.DemoRationale, FirstStep: best.DemoFirstStep}, nil
}

func (Fixture) Answer(_ context.Context, question string, a Assignment, _ []Turn) (string, error) {
	if domain.AsksAboutProgress(question) {
		return ProgressAnswer(a), nil
	}
	return GuideAnswer(a), nil
}

// GuideAnswer is the first step plus the instruction. Without an instruction it
// says so and gives a safe step instead of inventing links.
func GuideAnswer(a Assignment) string {
	if len(a.Guide) == 0 {
		return "Первый шаг: " + a.FirstStep + "\nВ тестовой базе знаний нет доступной тебе инструкции по " + a.TicketID +
			", поэтому ссылок на инструменты я не дам. Безопасный шаг: перечитай описание задачи в Jira " +
			"и спроси наставника, где лежат материалы."
	}
	text := "Первый шаг: " + a.FirstStep + "\nПо инструкции «" + a.Instruction + "» (тестовый ресурс):"
	for i, step := range a.Guide {
		text += fmt.Sprintf("\n%d. %s", i+1, step)
	}
	return text
}

func (Fixture) AssessBlocker(_ context.Context, c BlockerCase) (Assessment, error) {
	fix, matched := domain.MatchFix(c.Fixes, c.Answer)
	if !matched {
		fix, matched = domain.MatchFix(c.Fixes, c.Obstacle)
	}
	switch {
	case domain.AsksForHuman(c.Answer):
		request := "Свяжись со стажёром и помоги разобраться: «" + c.Answer + "»."
		if matched && fix.NeedsHuman {
			request = fix.Request
		}
		return Assessment{NeedsHuman: true, SelfStep: orDefault(fix.SelfStep, c.Check), Request: request}, nil
	case matched:
		return Assessment{NeedsHuman: fix.NeedsHuman, SelfStep: fix.SelfStep, Request: fix.Request}, nil
	default:
		return Assessment{NeedsHuman: true, SelfStep: c.Check,
			Request: "Свяжись со стажёром и помоги разобраться: «" + c.Answer + "»."}, nil
	}
}

func orDefault(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// ProgressAnswer explains status, deadline and how the status changes. It never claims a change.
func ProgressAnswer(a Assignment) string {
	text := "Сейчас у " + a.TicketID + " статус «" + a.Status + "», срок — " + a.Due + "."
	if a.Action != "" {
		text += " Перевести задачу дальше можно кнопкой «" + a.Action + "» над чатом — переход сохранится в Jira."
	}
	return text + "\nСледующий шаг: " + a.NextStep
}
