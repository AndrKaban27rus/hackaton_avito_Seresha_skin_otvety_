package store

import (
	"context"
	"embed"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Step struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type Store struct{ DB *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err = p.Ping(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return &Store{DB: p}, nil
}
func (s *Store) Close() { s.DB.Close() }
func Migrate(ctx context.Context, s *Store) error {
	b, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, string(b))
	return err
}
func (s *Store) ListSteps(ctx context.Context) ([]Step, error) {
	rows, err := s.DB.Query(ctx, `SELECT id,title,description,status,completed_at FROM onboarding_steps ORDER BY sort_order`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Step{}
	for rows.Next() {
		var x Step
		if err = rows.Scan(&x.ID, &x.Title, &x.Description, &x.Status, &x.CompletedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) CompleteStep(ctx context.Context, id string) (Step, error) {
	var x Step
	err := s.DB.QueryRow(ctx, `UPDATE onboarding_steps SET status='completed',completed_at=COALESCE(completed_at,now()) WHERE id=$1 RETURNING id,title,description,status,completed_at`, id).Scan(&x.ID, &x.Title, &x.Description, &x.Status, &x.CompletedAt)
	return x, err
}
func (s *Store) SaveMessage(ctx context.Context, role, content string) error {
	_, err := s.DB.Exec(ctx, `INSERT INTO chat_messages(role,content) VALUES($1,$2)`, role, content)
	return err
}
