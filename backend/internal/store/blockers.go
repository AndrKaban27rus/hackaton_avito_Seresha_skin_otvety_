package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

func (q *Q) upsertDoc(d domain.Doc, order int) error {
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = q.tx.Exec(q.ctx, `INSERT INTO kb_docs (id, kind, ticket_id, title, requires, body, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET kind = excluded.kind, ticket_id = excluded.ticket_id, title = excluded.title,
		requires = excluded.requires, body = excluded.body, sort_order = excluded.sort_order`,
		d.ID, d.Kind, d.TicketID, d.Title, d.Requires, body, order)
	return err
}

func collectDocs(rows pgx.Rows, err error) ([]domain.Doc, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Doc, error) {
		var raw []byte
		var requires []string
		var d domain.Doc
		if err := r.Scan(&raw, &requires); err != nil {
			return d, err
		}
		err := json.Unmarshal(raw, &d)
		// The column is the source of truth for access, not the JSON copy.
		d.Requires = requires
		return d, err
	})
}

// Docs lists the resources of a ticket in fixture order.
func (q *Q) Docs(ticketID string) ([]domain.Doc, error) {
	return collectDocs(q.tx.Query(q.ctx, `SELECT body, requires FROM kb_docs WHERE ticket_id = $1 ORDER BY sort_order`, ticketID))
}

// Doc returns a resource with the team of its ticket.
func (q *Q) Doc(id string) (domain.Doc, string, bool, error) {
	var raw []byte
	var requires []string
	var team string
	err := q.tx.QueryRow(q.ctx, `SELECT d.body, d.requires, t.team FROM kb_docs d JOIN jira_tickets t ON t.id = d.ticket_id
		WHERE d.id = $1`, id).Scan(&raw, &requires, &team)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Doc{}, "", false, nil
	}
	if err != nil {
		return domain.Doc{}, "", false, err
	}
	var d domain.Doc
	err = json.Unmarshal(raw, &d)
	d.Requires = requires
	return d, team, err == nil, err
}

// Blocker statuses.
const (
	BlockerClarifying = "clarifying"
	BlockerOpen       = "open"
	BlockerSelfHelp   = "self_help"
	BlockerClosed     = "closed"
)

type Blocker struct {
	ID         int64      `json:"id"`
	Intern     string     `json:"-"`
	InternName string     `json:"intern_name,omitempty"`
	TicketID   string     `json:"ticket_id"`
	HelperID   string     `json:"-"`
	HelperName string     `json:"helper_name,omitempty"`
	Status     string     `json:"status"`
	Obstacle   string     `json:"obstacle"`
	Details    string     `json:"details,omitempty"`
	Request    string     `json:"request,omitempty"`
	Message    string     `json:"message,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	OpenedAt   *time.Time `json:"opened_at,omitempty"`
}

const blockerSelect = `SELECT b.id, b.intern, i.name, b.ticket_id, b.helper_id, h.name, b.status, b.obstacle,
	b.details, b.request, b.message, b.created_at, b.opened_at
	FROM blockers b JOIN profiles i ON i.id = b.intern JOIN profiles h ON h.id = b.helper_id`

func collectBlockers(rows pgx.Rows, err error) ([]Blocker, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Blocker, error) {
		var b Blocker
		return b, r.Scan(&b.ID, &b.Intern, &b.InternName, &b.TicketID, &b.HelperID, &b.HelperName, &b.Status,
			&b.Obstacle, &b.Details, &b.Request, &b.Message, &b.CreatedAt, &b.OpenedAt)
	})
}

func first(bs []Blocker, err error) (*Blocker, error) {
	if err != nil || len(bs) == 0 {
		return nil, err
	}
	return &bs[0], nil
}

// ActiveBlocker returns the clarifying or open blocker of an intern's task.
func (q *Q) ActiveBlocker(intern, ticketID string) (*Blocker, error) {
	return first(collectBlockers(q.tx.Query(q.ctx, blockerSelect+` WHERE b.intern = $1 AND b.ticket_id = $2
		AND b.status IN ('clarifying', 'open')`, intern, ticketID)))
}

func (q *Q) Blocker(id int64) (*Blocker, error) {
	return first(collectBlockers(q.tx.Query(q.ctx, blockerSelect+` WHERE b.id = $1`, id)))
}

// OpenBlockersFor lists open help requests addressed to the helper.
func (q *Q) OpenBlockersFor(helper string) ([]Blocker, error) {
	return collectBlockers(q.tx.Query(q.ctx, blockerSelect+` WHERE b.helper_id = $1 AND b.status = 'open' ORDER BY b.id`, helper))
}

func (q *Q) InsertBlocker(intern, ticketID, helper, obstacle string) error {
	_, err := q.tx.Exec(q.ctx, `INSERT INTO blockers (intern, ticket_id, helper_id, status, obstacle)
		VALUES ($1, $2, $3, 'clarifying', $4)`, intern, ticketID, helper, obstacle)
	return err
}

// ResolveClarification moves a clarifying blocker to open or self_help.
func (q *Q) ResolveClarification(id int64, status, details, request, message string) (bool, error) {
	tag, err := q.tx.Exec(q.ctx, `UPDATE blockers SET status = $2, details = $3, request = $4, message = $5,
		opened_at = CASE WHEN $2 = 'open' THEN now() END,
		closed_at = CASE WHEN $2 = 'self_help' THEN now() END
		WHERE id = $1 AND status = 'clarifying'`, id, status, details, request, message)
	return tag.RowsAffected() == 1, err
}

// CloseBlocker closes an open blocker and reports whether it changed.
func (q *Q) CloseBlocker(id int64) (bool, error) {
	tag, err := q.tx.Exec(q.ctx, `UPDATE blockers SET status = 'closed', closed_at = now()
		WHERE id = $1 AND status IN ('open', 'clarifying')`, id)
	return tag.RowsAffected() == 1, err
}

func (q *Q) DeleteBlockers(intern string) error {
	_, err := q.tx.Exec(q.ctx, `DELETE FROM blockers WHERE intern = $1`, intern)
	return err
}
