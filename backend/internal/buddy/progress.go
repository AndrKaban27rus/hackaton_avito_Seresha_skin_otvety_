package buddy

import (
	"context"
	"fmt"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

// Buttons that change the status; the UI shows the same labels.
var actionLabels = map[string]string{
	domain.StatusAssigned:   "Взять в работу",
	domain.StatusInProgress: "Отправить на ревью",
}

func nextStep(status, firstStep string) string {
	switch status {
	case domain.StatusAssigned:
		return firstStep + " Когда начнёшь, нажми «Взять в работу»."
	case domain.StatusInProgress:
		return "Когда результат готов, приложи его к задаче в Jira и нажми «Отправить на ревью»."
	default:
		return "Задача на ревью. Дождись комментариев ревьюера и ответь на них в Jira."
	}
}

// ClockView is the shared demo date.
type ClockView struct {
	Today      string `json:"today"`
	Label      string `json:"label"`
	OffsetDays int    `json:"offset_days"`
}

func (s *Service) today(q *store.Q) (domain.Day, int, error) {
	offset, err := q.ClockOffset()
	if err != nil {
		return domain.Day{}, 0, err
	}
	return domain.Today(s.Now(), offset), offset, nil
}

func clockView(today domain.Day, offset int) ClockView {
	return ClockView{Today: today.String(), Label: today.Label(), OffsetDays: offset}
}

// TeamRow is one intern's task as the lead sees it.
type TeamRow struct {
	InternID   string `json:"intern_id"`
	Name       string `json:"name"`
	TicketID   string `json:"ticket_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status"`
	StatusName string `json:"status_name"`
	DueDate    string `json:"due_date,omitempty"`
	DueLabel   string `json:"due_label,omitempty"`
	Overdue    bool   `json:"overdue"`
}

// StatusNone marks an intern without a task in the lead's table.
const StatusNone = "none"

func (s *Service) team(q *store.Q, lead string, today domain.Day) ([]TeamRow, error) {
	interns, err := q.InternsOf(lead)
	if err != nil {
		return nil, err
	}
	rows := []TeamRow{}
	for _, p := range interns {
		a, err := q.Assignment(p.ID)
		if err != nil {
			return nil, err
		}
		row := TeamRow{InternID: p.ID, Name: p.Name, Status: StatusNone, StatusName: "Без задачи"}
		if v := assignmentView(a, p, today); v != nil {
			row.TicketID, row.Title, row.Status = v.TicketID, v.Title, v.Status
			row.StatusName = v.StatusName
			row.DueDate, row.DueLabel, row.Overdue = v.DueDate, v.DueLabel, v.Overdue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Transition moves the intern's own task one step forward after an explicit confirmation.
func (s *Service) Transition(ctx context.Context, user, ticketID, to string) error {
	if !domain.KnownStatus(to) {
		return problem(400, "Неизвестный статус задачи.")
	}
	return s.store.Write(ctx, func(q *store.Q) error {
		p, err := loadProfile(q, user)
		if err != nil {
			return err
		}
		if p.Role != domain.RoleIntern {
			return problem(403, "Статус меняет стажёр, которому назначена задача.")
		}
		a, err := q.Assignment(user)
		if err != nil {
			return err
		}
		if a == nil || a.Ticket.ID != ticketID || !domain.Permitted(a.Ticket, p) {
			return problem(403, "Менять статус можно только у своей назначенной задачи.")
		}
		if a.Status == to {
			return nil
		}
		next, ok := domain.NextStatus(a.Status)
		if !ok || next != to {
			return problem(409, fmt.Sprintf("Из статуса «%s» нельзя перейти в «%s».",
				domain.StatusName(a.Status), domain.StatusName(to)))
		}
		moved, err := q.UpdateStatus(user, ticketID, a.Status, to)
		if err != nil {
			return err
		}
		if !moved {
			return problem(409, "Статус задачи уже изменился. Обновите страницу.")
		}
		text := fmt.Sprintf("Jira: %s переведена в «%s».\nСледующий шаг: %s",
			ticketID, domain.StatusName(to), nextStep(to, a.FirstStep))
		_, err = q.InsertEvent(user, "status", fmt.Sprintf("%s:status:%s:%s", user, ticketID, to), text)
		return err
	})
}

// RunReminders sends due reminders for every assignment. Dedup keys make
// repeated runs, restarts and profile switches harmless.
func (s *Service) RunReminders(ctx context.Context) (int, error) {
	sent := 0
	err := s.store.Write(ctx, func(q *store.Q) error {
		sent = 0
		today, _, err := s.today(q)
		if err != nil {
			return err
		}
		all, err := q.Assignments()
		if err != nil {
			return err
		}
		for _, a := range all {
			p, ok, err := q.Profile(a.Intern)
			if err != nil {
				return err
			}
			if !ok || !domain.Permitted(a.Ticket, p) {
				continue
			}
			kind, ok := domain.ReminderFor(a.Status, a.Due, today)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s:%s:%s:%s", a.Intern, kind, a.Ticket.ID, a.Due)
			added, err := q.InsertEvent(a.Intern, kind, key, reminderText(kind, a, today))
			if err != nil {
				return err
			}
			if added {
				sent++
			}
		}
		return nil
	})
	return sent, err
}

func reminderText(kind string, a store.Assignment, today domain.Day) string {
	status := domain.StatusName(a.Status)
	step := nextStep(a.Status, a.FirstStep)
	if kind == domain.ReminderOverdue {
		return fmt.Sprintf("Срок по %s «%s» прошёл %s. Статус в Jira: «%s».\n"+
			"Если работа застряла, напиши мне, что мешает. Следующий шаг: %s",
			a.Ticket.ID, a.Ticket.Title, a.Due.Label(), status, step)
	}
	when := "завтра"
	if today.Equal(a.Due) {
		when = "сегодня"
	}
	return fmt.Sprintf("Напоминание: %s, %s, срок по %s «%s». Статус в Jira: «%s».\nСледующий шаг: %s",
		when, a.Due.Label(), a.Ticket.ID, a.Ticket.Title, status, step)
}

// Clock moves the shared demo date and runs the reminder check at once.
func (s *Service) Clock(ctx context.Context, action string) error {
	if action != "next_day" && action != "reset" {
		return problem(400, "Неизвестное действие с демо-временем.")
	}
	err := s.store.Write(ctx, func(q *store.Q) error {
		offset, err := q.ClockOffset()
		if err != nil {
			return err
		}
		if action == "reset" {
			offset = 0
		} else if offset >= 60 {
			return problem(409, "Демо-время можно сдвинуть не больше чем на 60 дней.")
		} else {
			offset++
		}
		return q.SetClockOffset(offset)
	})
	if err != nil {
		return err
	}
	_, err = s.RunReminders(ctx)
	return err
}
