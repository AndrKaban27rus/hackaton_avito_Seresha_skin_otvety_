package buddy

import (
	"context"
	"fmt"
	"strings"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/llm"
	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

// ResourceView is a synthetic tool link shown in the task panel.
type ResourceView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// readableDocs keeps only resources the profile may open.
func readableDocs(q *store.Q, a *store.Assignment, p domain.Profile) ([]domain.Doc, error) {
	if a == nil || !domain.Permitted(a.Ticket, p) {
		return nil, nil
	}
	docs, err := q.Docs(a.Ticket.ID)
	if err != nil {
		return nil, err
	}
	out := []domain.Doc{}
	for _, d := range docs {
		if domain.CanRead(d, p, a.Ticket.Team) {
			out = append(out, d)
		}
	}
	return out, nil
}

func resourceViews(docs []domain.Doc) []ResourceView {
	out := []ResourceView{}
	for _, d := range docs {
		out = append(out, ResourceView{ID: d.ID, Kind: d.Kind, Title: d.Title, URL: d.URL()})
	}
	return out
}

// helperFor picks who receives help requests: the mentor, or the lead without one.
func helperFor(q *store.Q, p domain.Profile) (domain.Profile, error) {
	for _, id := range []string{p.MentorID, p.LeadID} {
		if id == "" {
			continue
		}
		h, ok, err := q.Profile(id)
		if err != nil {
			return h, err
		}
		if ok && (h.Role == domain.RoleMentor || h.Role == domain.RoleLead) {
			return h, nil
		}
	}
	return domain.Profile{}, problem(503, "Наставник стажёра недоступен. Повторите запрос позже.")
}

// Resource returns a demo resource after the server-side access check.
func (s *Service) Resource(ctx context.Context, user, id string) (domain.Doc, error) {
	var doc domain.Doc
	err := s.store.Read(ctx, func(q *store.Q) error {
		p, err := loadProfile(q, user)
		if err != nil {
			return err
		}
		d, team, ok, err := q.Doc(id)
		if err != nil {
			return err
		}
		if !ok {
			return problem(404, "Тестовый ресурс не найден.")
		}
		if !domain.CanRead(d, p, team) {
			return problem(403, "У этого профиля нет допуска к тестовому ресурсу.")
		}
		doc = d
		return nil
	})
	return doc, err
}

// blockerPlan is what Chat decided about a blocker before the write.
type blockerPlan struct {
	create     bool
	resolve    bool
	assessment llm.Assessment
	answer     string
}

func clarifyQuestion(ticketID, obstacle string, instruction *domain.Doc) string {
	check := "перечитай описание задачи в Jira и проверь, что повторяешь шаги в том же порядке."
	if instruction != nil && instruction.Check != "" {
		check = instruction.Check
	}
	return fmt.Sprintf("Понял, по %s: «%s». Уточни, что именно мешает: нет доступа, не включается настройка, "+
		"ошибка в данных или что-то другое?\nЧто можно проверить самому: %s", ticketID, obstacle, check)
}

func openBlockerReply(b *store.Blocker) string {
	return fmt.Sprintf("Блокер по %s уже передан наставнику — %s. Повторно писать не нужно.\nПросьба к наставнику: %s\n"+
		"Когда препятствие уйдёт, нажми «Блокер снят» над чатом.", b.TicketID, b.HelperName, b.Request)
}

// resolveBlocker stores the assessed clarification and notifies the helper if needed.
func (s *Service) resolveBlocker(q *store.Q, snap snapshot, plan blockerPlan) (string, error) {
	b := snap.blocker
	a := snap.assignment
	if !plan.assessment.NeedsHuman {
		if _, err := q.ResolveClarification(b.ID, store.BlockerSelfHelp, plan.answer, "", ""); err != nil {
			return "", err
		}
		return "Это можно решить самому, наставника не зову.\n" + plan.assessment.SelfStep +
			"\nЕсли не поможет, напиши, что именно не получилось.", nil
	}
	p := snap.profile
	message := fmt.Sprintf("Нужна помощь стажёру\nСтажёр: %s · стажёр %s · команда «%s»\n"+
		"Задача: %s «%s» · статус «%s» · срок %s\nПрепятствие: %s. %s\nПросьба: %s",
		p.Name, p.Grade, p.Team, a.TicketID, a.Title, a.StatusName, a.DueLabel,
		strings.TrimRight(b.Obstacle, ".!"), plan.answer, plan.assessment.Request)
	if _, err := q.ResolveClarification(b.ID, store.BlockerOpen, plan.answer, plan.assessment.Request, message); err != nil {
		return "", err
	}
	return fmt.Sprintf("Записал блокер по %s и передал его наставнику — %s.\nПросьба к наставнику: %s\n"+
		"Что можно сделать сейчас: %s\nКогда препятствие уйдёт, нажми «Блокер снят» над чатом.",
		a.TicketID, b.HelperName, plan.assessment.Request, plan.assessment.SelfStep), nil
}

// CloseBlocker lets the intern or the helper close an open blocker.
func (s *Service) CloseBlocker(ctx context.Context, user string, id int64) error {
	return s.store.Write(ctx, func(q *store.Q) error {
		if _, err := loadProfile(q, user); err != nil {
			return err
		}
		b, err := q.Blocker(id)
		if err != nil {
			return err
		}
		if b == nil || (b.Intern != user && b.HelperID != user) {
			return problem(403, "Снять можно только свой блокер или блокер своего стажёра.")
		}
		closed, err := q.CloseBlocker(id)
		if err != nil || !closed {
			return err
		}
		text := fmt.Sprintf("Блокер по %s снят. Продолжай работу по задаче, а если появится новое препятствие — напиши мне.", b.TicketID)
		_, err = q.InsertEvent(b.Intern, "blocker", fmt.Sprintf("%s:blocker_closed:%d", b.Intern, b.ID), text)
		return err
	})
}
