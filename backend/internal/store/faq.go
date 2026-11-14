package store

import (
	"github.com/jackc/pgx/v5"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

func (q *Q) upsertFAQ(f domain.FAQ, order int) error {
	match := f.Match
	if match == nil {
		match = []string{}
	}
	_, err := q.tx.Exec(q.ctx, `INSERT INTO faq (id, team, topic, question, answer, keywords, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET team = excluded.team, topic = excluded.topic, question = excluded.question,
		answer = excluded.answer, keywords = excluded.keywords, sort_order = excluded.sort_order`,
		f.ID, f.Team, f.Topic, f.Question, f.Answer, match, order)
	return err
}

// FAQ lists the frequent questions of a team in fixture order.
func (q *Q) FAQ(team string) ([]domain.FAQ, error) {
	rows, err := q.tx.Query(q.ctx, `SELECT id, team, topic, question, answer, keywords FROM faq
		WHERE team = $1 ORDER BY sort_order`, team)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.FAQ, error) {
		var f domain.FAQ
		return f, r.Scan(&f.ID, &f.Team, &f.Topic, &f.Question, &f.Answer, &f.Match)
	})
}
