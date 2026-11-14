// Command metrics prints pilot metrics from the demo database:
// time to the first independent step, questions to the bot and blockers.
//
//	DATABASE_URL=postgres://... go run ./cmd/metrics
//
// Without DATABASE_URL it connects to the local PostgreSQL started by the API.
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "metrics:", err)
		os.Exit(1)
	}
}

func run(out *os.File) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		port := os.Getenv("LOCAL_PG_PORT")
		if port == "" {
			port = "54329"
		}
		url = "postgres://buddy:buddy@127.0.0.1:" + port + "/buddy?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.Metrics(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Стажёр\tЗадача\tДо «В работе»\tДо «На ревью»\tВопросов боту\tБлокеров к наставнику\tРешено по инструкции")
	for _, m := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%d\t%d\n", m.Name, m.TicketID,
			since(m.AssignedAt, m.StartedAt), since(m.AssignedAt, m.ReviewAt), m.Questions, m.Escalated, m.SelfSolved)
	}
	return w.Flush()
}

func since(from time.Time, to *time.Time) string {
	if to == nil {
		return "—"
	}
	return to.Sub(from).Round(time.Minute).String()
}
