package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// InternMetrics are pilot numbers for one intern's current assignment.
// Timestamps are real time, not the demo clock.
type InternMetrics struct {
	InternID   string
	Name       string
	TicketID   string
	AssignedAt time.Time
	// StartedAt is the confirmed «В работе» transition: the first independent step.
	StartedAt *time.Time
	ReviewAt  *time.Time
	// Questions counts messages to the bot after the assignment; a chat reset removes them.
	Questions int
	// Escalated blockers reached the mentor; SelfSolved ended with a step from the instruction.
	Escalated  int
	SelfSolved int
}

// Metrics reads pilot metrics for every intern with an assignment.
func (q *Q) Metrics() ([]InternMetrics, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT p.id, p.name, a.ticket_id, a.created_at,
		(SELECT min(e.created_at) FROM bot_events e
			WHERE e.dedup_key = p.id || ':status:' || a.ticket_id || ':in_progress'),
		(SELECT min(e.created_at) FROM bot_events e
			WHERE e.dedup_key = p.id || ':status:' || a.ticket_id || ':review'),
		(SELECT count(*) FROM chat_turns t WHERE t.user_id = p.id AND t.created_at >= a.created_at),
		(SELECT count(*) FROM blockers b WHERE b.intern = p.id AND b.ticket_id = a.ticket_id AND b.opened_at IS NOT NULL),
		(SELECT count(*) FROM blockers b WHERE b.intern = p.id AND b.ticket_id = a.ticket_id AND b.status = 'self_help')
		FROM profiles p JOIN jira_assignments a ON a.intern = p.id
		WHERE p.role = 'intern' ORDER BY p.sort_order`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (InternMetrics, error) {
		var m InternMetrics
		return m, r.Scan(&m.InternID, &m.Name, &m.TicketID, &m.AssignedAt, &m.StartedAt, &m.ReviewAt,
			&m.Questions, &m.Escalated, &m.SelfSolved)
	})
}

// Metrics runs the metrics query in a read transaction.
func (s *Store) Metrics(ctx context.Context) ([]InternMetrics, error) {
	var out []InternMetrics
	err := s.Read(ctx, func(q *Q) error {
		var err error
		out, err = q.Metrics()
		return err
	})
	return out, err
}
