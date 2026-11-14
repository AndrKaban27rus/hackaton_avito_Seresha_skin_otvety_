// Package store keeps the demo state in PostgreSQL: the directory, the Jira
// simulation, the Slack chats and the lead inbox.
package store

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/seed"
)

//go:embed schema.sql
var schema string

// writeLock serialises demo writes, like a single Jira workflow transition queue.
const writeLock = 7243001

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Migrate creates the schema and loads synthetic data. Profiles follow the
// fixture; existing tickets keep their Jira state.
func (s *Store) Migrate(ctx context.Context, data seed.Data) error {
	return s.Write(ctx, func(q *Q) error {
		if _, err := q.tx.Exec(ctx, schema); err != nil {
			return err
		}
		for i, p := range data.Profiles {
			if _, err := q.tx.Exec(ctx, `INSERT INTO profiles (id, role, name, team, grade, grants, lead_id, sort_order, mentor_id)
				VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, NULLIF($9, ''))
				ON CONFLICT (id) DO UPDATE SET role = excluded.role, name = excluded.name, team = excluded.team,
				grade = excluded.grade, grants = excluded.grants, lead_id = excluded.lead_id, sort_order = excluded.sort_order,
				mentor_id = excluded.mentor_id`,
				p.ID, p.Role, p.Name, p.Team, p.Grade, p.Grants, p.LeadID, i, p.MentorID); err != nil {
				return err
			}
		}
		for i, t := range data.Tickets {
			if _, err := q.tx.Exec(ctx, `INSERT INTO jira_tickets (id, team, title, description, requires, labels,
				status, classification, grades, okr_priority, demo_rationale, demo_first_step, sort_order, due_days)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
				ON CONFLICT (id) DO UPDATE SET due_days = excluded.due_days`,
				t.ID, t.Team, t.Title, t.Description, t.Requires, t.Labels, t.Status, t.Classification,
				t.Grades, t.OKRPriority, t.DemoRationale, t.DemoFirstStep, i, dueDays(t.DueDays)); err != nil {
				return err
			}
		}
		for i, d := range data.Docs {
			if err := q.upsertDoc(d, i); err != nil {
				return err
			}
		}
		return nil
	})
}

func dueDays(n int) int {
	if n <= 0 {
		return domain.DefaultDueDays
	}
	return n
}

// Q runs queries inside one transaction.
type Q struct {
	ctx context.Context
	tx  pgx.Tx
}

// Read runs fn in a transaction without the write lock.
func (s *Store) Read(ctx context.Context, fn func(*Q) error) error {
	return pgx.BeginTxFunc(ctx, s.Pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		return fn(&Q{ctx: ctx, tx: tx})
	})
}

// Write runs fn in a transaction that holds the demo write lock.
func (s *Store) Write(ctx context.Context, fn func(*Q) error) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, writeLock); err != nil {
			return err
		}
		return fn(&Q{ctx: ctx, tx: tx})
	})
}

// IsUniqueViolation reports a broken uniqueness constraint.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (q *Q) Profile(id string) (domain.Profile, bool, error) {
	var p domain.Profile
	var lead, mentor *string
	err := q.tx.QueryRow(q.ctx, `SELECT id, role, name, team, grade, grants, lead_id, mentor_id FROM profiles WHERE id = $1`, id).
		Scan(&p.ID, &p.Role, &p.Name, &p.Team, &p.Grade, &p.Grants, &lead, &mentor)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	if lead != nil {
		p.LeadID = *lead
	}
	if mentor != nil {
		p.MentorID = *mentor
	}
	return p, err == nil, err
}

func (q *Q) Profiles() ([]domain.Profile, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT id, role, name, grade FROM profiles ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Profile, error) {
		var p domain.Profile
		return p, r.Scan(&p.ID, &p.Role, &p.Name, &p.Grade)
	})
}

const ticketColumns = `id, team, title, description, requires, labels, status, classification, grades,
	okr_priority, demo_rationale, demo_first_step, due_days`

func scanTicket(r pgx.Row) (domain.Ticket, error) {
	var t domain.Ticket
	err := r.Scan(&t.ID, &t.Team, &t.Title, &t.Description, &t.Requires, &t.Labels, &t.Status,
		&t.Classification, &t.Grades, &t.OKRPriority, &t.DemoRationale, &t.DemoFirstStep, &t.DueDays)
	return t, err
}

// Tickets lists the Jira backlog in fixture order.
func (q *Q) Tickets() ([]domain.Ticket, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT `+ticketColumns+` FROM jira_tickets ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Ticket, error) { return scanTicket(r) })
}

// Taken returns IDs of tickets that already have an assignee.
func (q *Q) Taken() (map[string]bool, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT ticket_id FROM jira_assignments`)
	if err != nil {
		return nil, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	taken := map[string]bool{}
	for _, id := range ids {
		taken[id] = true
	}
	return taken, err
}

type Assignment struct {
	Intern    string
	Ticket    domain.Ticket
	Status    string
	Rationale string
	FirstStep string
	Mode      string
	Due       domain.Day
	CreatedAt time.Time
}

const assignmentSelect = `SELECT a.intern, t.id, t.team, t.title, t.description, t.requires, t.labels, t.status,
	t.classification, t.grades, t.okr_priority, t.demo_rationale, t.demo_first_step, t.due_days,
	a.status, a.rationale, a.first_step, a.mode, a.due_date, a.created_at
	FROM jira_assignments a JOIN jira_tickets t ON t.id = a.ticket_id`

func scanAssignment(r pgx.Row) (Assignment, error) {
	var a Assignment
	var due time.Time
	t := &a.Ticket
	err := r.Scan(&a.Intern, &t.ID, &t.Team, &t.Title, &t.Description, &t.Requires, &t.Labels, &t.Status,
		&t.Classification, &t.Grades, &t.OKRPriority, &t.DemoRationale, &t.DemoFirstStep, &t.DueDays,
		&a.Status, &a.Rationale, &a.FirstStep, &a.Mode, &due, &a.CreatedAt)
	a.Due = domain.DayFromTime(due)
	return a, err
}

func (q *Q) Assignment(intern string) (*Assignment, error) {
	a, err := scanAssignment(q.tx.QueryRow(q.ctx, assignmentSelect+` WHERE a.intern = $1`, intern))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Assignments lists every assignment, for the reminder scheduler.
func (q *Q) Assignments() ([]Assignment, error) {
	rows, err := q.tx.Query(q.ctx, assignmentSelect+` ORDER BY a.intern`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Assignment, error) { return scanAssignment(r) })
}

func (q *Q) InsertAssignment(intern, ticketID, rationale, firstStep, mode string, due domain.Day) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO jira_assignments (intern, ticket_id, rationale, first_step, mode, due_date)
		VALUES ($1, $2, $3, $4, $5, $6)`, intern, ticketID, rationale, firstStep, mode, due.Time())
	return err
}

// UpdateStatus moves the assignment only if it is still in the expected status.
func (q *Q) UpdateStatus(intern, ticketID, from, to string) (bool, error) {
	tag, err := q.tx.Exec(q.ctx, `UPDATE jira_assignments SET status = $4, status_changed_at = now()
		WHERE intern = $1 AND ticket_id = $2 AND status = $3`, intern, ticketID, from, to)
	return tag.RowsAffected() == 1, err
}

// InternsOf lists the lead's interns in directory order.
func (q *Q) InternsOf(lead string) ([]domain.Profile, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT id, role, name, team, grade, grants FROM profiles
		WHERE lead_id = $1 AND role = 'intern' ORDER BY sort_order`, lead)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Profile, error) {
		p := domain.Profile{LeadID: lead}
		return p, r.Scan(&p.ID, &p.Role, &p.Name, &p.Team, &p.Grade, &p.Grants)
	})
}

func (q *Q) ClockOffset() (int, error) {
	var n int
	err := q.tx.QueryRow(q.ctx, `SELECT offset_days FROM demo_clock WHERE id = 1`).Scan(&n)
	return n, err
}

func (q *Q) SetClockOffset(n int) error {
	_, err := q.tx.Exec(q.ctx, `UPDATE demo_clock SET offset_days = $1 WHERE id = 1`, n)
	return err
}

type Event struct {
	ID        int64     `json:"-"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertEvent adds a bot message once per dedup key and reports whether it was new.
func (q *Q) InsertEvent(user, kind, key, text string) (bool, error) {
	tag, err := q.tx.Exec(q.ctx, `INSERT INTO bot_events (user_id, kind, dedup_key, text) VALUES ($1, $2, $3, $4)
		ON CONFLICT (dedup_key) DO NOTHING`, user, kind, key, text)
	return tag.RowsAffected() == 1, err
}

// Events returns visible bot messages, oldest first.
func (q *Q) Events(user string) ([]Event, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT id, kind, text, created_at FROM bot_events
		WHERE user_id = $1 AND NOT hidden ORDER BY id`, user)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		return e, r.Scan(&e.ID, &e.Kind, &e.Text, &e.CreatedAt)
	})
}

// HideEvents clears the chat view but keeps dedup keys, so nothing is resent.
func (q *Q) HideEvents(user string) error {
	_, err := q.tx.Exec(q.ctx, `UPDATE bot_events SET hidden = true WHERE user_id = $1`, user)
	return err
}

func (q *Q) DeleteEvents(user string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM bot_events WHERE user_id = $1`, user)
	return err
}

func (q *Q) DeleteAssignment(intern string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM jira_assignments WHERE intern = $1`, intern)
	return err
}

type Turn struct {
	ID        int64     `json:"-"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Mode      string    `json:"mode"`
	CreatedAt time.Time `json:"created_at"`
}

// TurnQuestion returns the question stored for a request ID, if any.
func (q *Q) TurnQuestion(user, requestID string) (string, bool, error) {
	var question string
	err := q.tx.QueryRow(q.ctx, `SELECT question FROM chat_turns WHERE user_id = $1 AND request_id = $2`,
		user, requestID).Scan(&question)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return question, err == nil, err
}

func collectTurns(rows pgx.Rows, err error) ([]Turn, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Turn, error) {
		var t Turn
		return t, r.Scan(&t.ID, &t.Question, &t.Answer, &t.Mode, &t.CreatedAt)
	})
}

// Turns returns the whole chat, oldest first.
func (q *Q) Turns(user string) ([]Turn, error) {
	return collectTurns(q.tx.Query(q.ctx, `SELECT id, question, answer, mode, created_at FROM chat_turns
		WHERE user_id = $1 ORDER BY id`, user))
}

// RecentTurns returns up to limit latest turns, newest first.
func (q *Q) RecentTurns(user string, limit int) ([]Turn, error) {
	return collectTurns(q.tx.Query(q.ctx, `SELECT id, question, answer, mode, created_at FROM chat_turns
		WHERE user_id = $1 ORDER BY id DESC LIMIT $2`, user, limit))
}

func (q *Q) LatestTurnID(user string) (int64, error) {
	var id int64
	err := q.tx.QueryRow(q.ctx, `SELECT COALESCE(MAX(id), 0) FROM chat_turns WHERE user_id = $1`, user).Scan(&id)
	return id, err
}

func (q *Q) InsertTurn(user, requestID, question, answer, mode string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO chat_turns (user_id, request_id, question, answer, mode)
		VALUES ($1, $2, $3, $4, $5)`, user, requestID, question, answer, mode)
	return err
}

func (q *Q) DeleteTurns(user string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM chat_turns WHERE user_id = $1`, user)
	return err
}

func (q *Q) ChatVersion(user string) (int64, error) {
	var v int64
	err := q.tx.QueryRow(q.ctx, `SELECT COALESCE((SELECT version FROM chat_versions WHERE user_id = $1), 0)`, user).Scan(&v)
	return v, err
}

func (q *Q) BumpChatVersion(user string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO chat_versions (user_id, version) VALUES ($1, 1)
		ON CONFLICT (user_id) DO UPDATE SET version = chat_versions.version + 1`, user)
	return err
}

type Report struct {
	ID        int64     `json:"-"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// InsertReport adds a report unless this intern already has one with the same outcome.
func (q *Q) InsertReport(lead, intern, outcome, message string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO lead_inbox (lead_id, intern, outcome, message) VALUES ($1, $2, $3, $4)
		ON CONFLICT (intern, outcome) DO NOTHING`, lead, intern, outcome, message)
	return err
}

func (q *Q) DeleteReports(intern string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM lead_inbox WHERE intern = $1`, intern)
	return err
}

// Reports returns reports the lead has not cleared, oldest first.
func (q *Q) Reports(lead string) ([]Report, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT id, message, created_at FROM lead_inbox WHERE lead_id = $1
		AND id > COALESCE((SELECT last_report_id FROM inbox_resets WHERE lead_id = $1), 0) ORDER BY id`, lead)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Report, error) {
		var x Report
		return x, r.Scan(&x.ID, &x.Message, &x.CreatedAt)
	})
}

// ClearInbox hides every report the lead has received so far.
func (q *Q) ClearInbox(lead string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO inbox_resets (lead_id, last_report_id)
		SELECT $1, COALESCE(MAX(id), 0) FROM lead_inbox WHERE lead_id = $1
		ON CONFLICT (lead_id) DO UPDATE SET last_report_id = GREATEST(inbox_resets.last_report_id, excluded.last_report_id)`, lead)
	return err
}

func (q *Q) CreateSession(tokenHash, user string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO sessions (token_hash, user_id) VALUES ($1, $2)`, tokenHash, user)
	return err
}

func (q *Q) DeleteSession(tokenHash string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (q *Q) SessionUser(tokenHash string) (string, bool, error) {
	var user string
	err := q.tx.QueryRow(q.ctx, `SELECT user_id FROM sessions WHERE token_hash = $1`, tokenHash).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return user, err == nil, err
}
