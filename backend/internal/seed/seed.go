// Package seed loads the synthetic demo directory, backlog and channels.
package seed

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
)

//go:embed demo_data.json
var raw []byte

type Message struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

type Channel struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Messages []Message `json:"messages"`
}

type Data struct {
	Profiles []domain.Profile
	Tickets  []domain.Ticket
	TeamGoal string
	Channels []Channel
	Docs     []domain.Doc
	FAQ      []domain.FAQ
}

type profileJSON struct {
	Role     string   `json:"role"`
	Name     string   `json:"name"`
	Team     string   `json:"team"`
	Grade    *string  `json:"grade"`
	Grants   []string `json:"grants"`
	LeadID   string   `json:"lead_id"`
	MentorID string   `json:"mentor_id"`
}

type ticketJSON struct {
	ID             string   `json:"id"`
	Team           string   `json:"team"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Requires       []string `json:"requires"`
	Labels         []string `json:"labels"`
	Status         string   `json:"status"`
	Classification string   `json:"classification"`
	Grades         []string `json:"grades"`
	OKRPriority    int      `json:"okr_priority"`
	DueDays        int      `json:"due_days"`
	DemoRationale  string   `json:"demo_rationale"`
	DemoFirstStep  string   `json:"demo_first_step"`
}

type fileJSON struct {
	Profiles orderedProfiles `json:"profiles"`
	Tickets  []ticketJSON    `json:"tickets"`
	TeamGoal string          `json:"team_goal"`
	Channels []Channel       `json:"channels"`
	Docs     []domain.Doc    `json:"knowledge_base"`
	FAQ      []domain.FAQ    `json:"faq"`
}

// orderedProfiles keeps the order of the JSON object so the profile picker is stable.
type orderedProfiles []domain.Profile

func (o *orderedProfiles) UnmarshalJSON(b []byte) error {
	var byID map[string]profileJSON
	if err := json.Unmarshal(b, &byID); err != nil {
		return err
	}
	var keys []string
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err != nil {
		return err
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	for _, id := range keys {
		p := byID[id]
		grade := ""
		if p.Grade != nil {
			grade = *p.Grade
		}
		grants := p.Grants
		if grants == nil {
			grants = []string{}
		}
		*o = append(*o, domain.Profile{ID: id, Role: p.Role, Name: p.Name, Team: p.Team, Grade: grade, Grants: grants, LeadID: p.LeadID, MentorID: p.MentorID})
	}
	return nil
}

// Load parses the embedded demo data.
func Load() (Data, error) { return parse(raw) }

func parse(b []byte) (Data, error) {
	var f fileJSON
	if err := json.Unmarshal(b, &f); err != nil {
		return Data{}, fmt.Errorf("demo data: %w", err)
	}
	d := Data{Profiles: f.Profiles, TeamGoal: f.TeamGoal, Channels: f.Channels, Docs: f.Docs, FAQ: f.FAQ}
	for _, t := range f.Tickets {
		labels := t.Labels
		if labels == nil {
			labels = []string{}
		}
		d.Tickets = append(d.Tickets, domain.Ticket{
			ID: t.ID, Team: t.Team, Title: t.Title, Description: t.Description,
			Requires: t.Requires, Labels: labels, Status: t.Status, Classification: t.Classification,
			Grades: t.Grades, OKRPriority: t.OKRPriority, DueDays: t.DueDays, DemoRationale: t.DemoRationale, DemoFirstStep: t.DemoFirstStep,
		})
	}
	return d, nil
}
