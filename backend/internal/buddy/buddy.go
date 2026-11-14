// Package buddy is the AI-buddy chat scenario: intent, safe ticket choice,
// assignment in Jira and the report to the lead.
package buddy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hackandchange/ai-buddy/backend/internal/domain"
	"github.com/hackandchange/ai-buddy/backend/internal/llm"
	"github.com/hackandchange/ai-buddy/backend/internal/seed"
	"github.com/hackandchange/ai-buddy/backend/internal/store"
)

const historyTurns = 8

// Problem is a user-facing error with an HTTP status.
type Problem struct {
	Status  int
	Message string
	// Cause is logged by the server and never shown to the user.
	Cause error
}

func (p *Problem) Error() string { return p.Message }

func problem(status int, msg string) error { return &Problem{Status: status, Message: msg} }

const (
	MsgNoneForProfile = "Сейчас нет стартовых задач для твоего грейда и допусков. Я сообщил руководителю."
	MsgAllTaken       = "Сейчас все стартовые задачи, которые тебе подходят, уже взяли другие стажёры. " +
		"Я сообщил руководителю. Попроси подобрать задачу позже."
	MsgRestricted = "Детали назначенной задачи сейчас недоступны. Обратись к руководителю."

	reportNone  = "Подходящая задача не найдена: среди открытых задач нет стартовых для этого грейда и допусков."
	reportTaken = "Подходящая задача не найдена: все стартовые задачи, доступные стажёру, уже назначены другим."
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

type Service struct {
	store *store.Store
	model llm.Model
	data  seed.Data
	// AfterModel lets tests change Jira between the model call and the write.
	AfterModel func()
	// Now is the real clock; the demo offset is added on top.
	Now func() time.Time
}

func New(s *store.Store, m llm.Model, d seed.Data) *Service {
	return &Service{store: s, model: m, data: d, Now: time.Now}
}

// AssignmentView is what the intern sees about the current assignment.
type AssignmentView struct {
	TicketID   string    `json:"ticket_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status"`
	StatusName string    `json:"status_name"`
	Rationale  string    `json:"rationale,omitempty"`
	FirstStep  string    `json:"first_step,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitzero"`
	DueDate    string    `json:"due_date,omitempty"`
	DueLabel   string    `json:"due_label,omitempty"`
	DaysLeft   int       `json:"days_left"`
	Overdue    bool      `json:"overdue"`
	NextStep   string    `json:"next_step,omitempty"`
	NextStatus string    `json:"next_status,omitempty"`
	Action     string    `json:"action,omitempty"`
	// Resources are synthetic tool links the intern may open.
	Resources []ResourceView `json:"resources,omitempty"`
}

// StatusRestricted hides a ticket the intern may no longer see.
const StatusRestricted = "restricted"

func assignmentView(a *store.Assignment, p domain.Profile, today domain.Day) *AssignmentView {
	if a == nil {
		return nil
	}
	if !domain.Permitted(a.Ticket, p) {
		return &AssignmentView{Status: StatusRestricted, StatusName: "Недоступна"}
	}
	v := &AssignmentView{TicketID: a.Ticket.ID, Title: a.Ticket.Title, Status: a.Status,
		StatusName: domain.StatusName(a.Status), Rationale: a.Rationale, FirstStep: a.FirstStep,
		Mode: a.Mode, CreatedAt: a.CreatedAt, DueDate: a.Due.String(), DueLabel: a.Due.Label(),
		DaysLeft: today.DaysUntil(a.Due), NextStep: nextStep(a.Status, a.FirstStep)}
	v.Overdue = v.DaysLeft < 0 && a.Status != domain.StatusReview
	if next, ok := domain.NextStatus(a.Status); ok {
		v.NextStatus, v.Action = next, actionLabels[a.Status]
	}
	return v
}

func sameAssignment(a, b *AssignmentView) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.TicketID == b.TicketID && a.Status == b.Status
}

type ProfileView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Team  string `json:"team,omitempty"`
	Grade string `json:"grade"`
}

type State struct {
	Profile       ProfileView     `json:"profile"`
	Profiles      []ProfileView   `json:"profiles"`
	Channels      []seed.Channel  `json:"channels"`
	Chat          []store.Turn    `json:"chat"`
	Notifications []store.Report  `json:"notifications"`
	Assignment    *AssignmentView `json:"assignment"`
	Events        []store.Event   `json:"events"`
	Team          []TeamRow       `json:"team"`
	Clock         ClockView       `json:"clock"`
	Blocker       *store.Blocker  `json:"blocker"`
	HelpRequests  []store.Blocker `json:"help_requests"`
	Mode          string          `json:"mode"`
}

func loadProfile(q *store.Q, user string) (domain.Profile, error) {
	p, ok, err := q.Profile(user)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, problem(401, "Выберите тестовый профиль.")
	}
	return p, nil
}

// State returns everything the UI needs for one profile.
func (s *Service) State(ctx context.Context, user string) (State, error) {
	var st State
	err := s.store.Read(ctx, func(q *store.Q) error {
		p, err := loadProfile(q, user)
		if err != nil {
			return err
		}
		st.Profile = ProfileView{ID: p.ID, Name: p.Name, Role: p.Role, Team: p.Team, Grade: p.Grade}
		profiles, err := q.Profiles()
		if err != nil {
			return err
		}
		for _, x := range profiles {
			st.Profiles = append(st.Profiles, ProfileView{ID: x.ID, Name: x.Name, Role: x.Role, Grade: x.Grade})
		}
		if st.Chat, err = q.Turns(user); err != nil {
			return err
		}
		if st.Notifications, err = q.Reports(user); err != nil {
			return err
		}
		today, offset, err := s.today(q)
		if err != nil {
			return err
		}
		st.Clock = clockView(today, offset)
		st.Events, st.Team, st.HelpRequests = []store.Event{}, []TeamRow{}, []store.Blocker{}
		if p.Role != domain.RoleIntern {
			// Help requests go to the mentor, or to the lead for interns without one.
			if st.HelpRequests, err = q.OpenBlockersFor(user); err != nil {
				return err
			}
		}
		if p.Role == domain.RoleLead {
			st.Team, err = s.team(q, user, today)
			return err
		}
		if p.Role == domain.RoleMentor {
			return nil
		}
		if st.Events, err = q.Events(user); err != nil {
			return err
		}
		a, err := q.Assignment(user)
		if err != nil {
			return err
		}
		st.Assignment = assignmentView(a, p, today)
		if st.Assignment == nil || st.Assignment.Status == StatusRestricted {
			return nil
		}
		docs, err := readableDocs(q, a, p)
		if err != nil {
			return err
		}
		st.Assignment.Resources = resourceViews(docs)
		st.Blocker, err = q.ActiveBlocker(user, a.Ticket.ID)
		return err
	})
	st.Channels = s.data.Channels
	st.Mode = s.model.Mode()
	return st, err
}

// Login checks the profile and creates a session.
func (s *Service) Login(ctx context.Context, profile, tokenHash, oldHash string) error {
	return s.store.Write(ctx, func(q *store.Q) error {
		if _, ok, err := q.Profile(profile); err != nil || !ok {
			if err != nil {
				return err
			}
			return problem(400, "Неизвестный тестовый профиль.")
		}
		if oldHash != "" {
			if err := q.DeleteSession(oldHash); err != nil {
				return err
			}
		}
		return q.CreateSession(tokenHash, profile)
	})
}

// SessionUser resolves a session to a profile ID.
func (s *Service) SessionUser(ctx context.Context, tokenHash string) (string, error) {
	var user string
	err := s.store.Read(ctx, func(q *store.Q) error {
		u, ok, err := q.SessionUser(tokenHash)
		if err == nil && !ok {
			return problem(401, "Выберите тестовый профиль.")
		}
		user = u
		return err
	})
	return user, err
}

// ResetChat clears the intern's chat or hides the lead's reports. With
// restartPick it also releases the intern's assignment and reports.
func (s *Service) ResetChat(ctx context.Context, user string, restartPick bool) error {
	return s.store.Write(ctx, func(q *store.Q) error {
		p, err := loadProfile(q, user)
		if err != nil {
			return err
		}
		if p.Role != domain.RoleIntern {
			if restartPick {
				return problem(403, "Начать подбор заново можно из профиля стажёра.")
			}
			return q.ClearInbox(user)
		}
		if restartPick {
			if err := q.DeleteBlockers(user); err != nil {
				return err
			}
			if err := q.DeleteAssignment(user); err != nil {
				return err
			}
			if err := q.DeleteReports(user); err != nil {
				return err
			}
			if err := q.DeleteEvents(user); err != nil {
				return err
			}
		} else if err := q.HideEvents(user); err != nil {
			return err
		}
		if err := q.DeleteTurns(user); err != nil {
			return err
		}
		return q.BumpChatVersion(user)
	})
}

func validate(message, requestID string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" || utf8.RuneCountInString(message) > 1200 {
		return "", problem(400, "Введите запрос длиной до 1200 символов.")
	}
	if !requestIDPattern.MatchString(requestID) {
		return "", problem(400, "Некорректный номер запроса.")
	}
	return message, nil
}

// seen reports whether the request was already answered.
func seen(q *store.Q, user, requestID, question string) (bool, error) {
	stored, ok, err := q.TurnQuestion(user, requestID)
	if err != nil || !ok {
		return false, err
	}
	if stored != question {
		return false, problem(409, "Этот номер запроса уже использован. Отправьте сообщение снова.")
	}
	return true, nil
}

type snapshot struct {
	profile    domain.Profile
	version    int64
	latest     int64
	assignment *AssignmentView
	history    []llm.Turn
	candidates []domain.Ticket
	today      domain.Day
	blocker    *store.Blocker
	docs       []domain.Doc
	done       bool
}

func (s *Service) read(ctx context.Context, user, requestID, question string) (snapshot, error) {
	var snap snapshot
	err := s.store.Read(ctx, func(q *store.Q) error {
		p, err := loadProfile(q, user)
		if err != nil {
			return err
		}
		if p.Role != domain.RoleIntern {
			return problem(403, "Подбор задачи доступен стажёру в личном чате с AI-buddy.")
		}
		snap.profile = p
		if snap.done, err = seen(q, user, requestID, question); err != nil || snap.done {
			return err
		}
		if snap.version, err = q.ChatVersion(user); err != nil {
			return err
		}
		a, err := q.Assignment(user)
		if err != nil {
			return err
		}
		if snap.today, _, err = s.today(q); err != nil {
			return err
		}
		snap.assignment = assignmentView(a, p, snap.today)
		if snap.assignment != nil && snap.assignment.Status != StatusRestricted {
			if snap.docs, err = readableDocs(q, a, p); err != nil {
				return err
			}
			if snap.blocker, err = q.ActiveBlocker(user, a.Ticket.ID); err != nil {
				return err
			}
		}
		recent, err := q.RecentTurns(user, historyTurns)
		if err != nil {
			return err
		}
		if len(recent) > 0 {
			snap.latest = recent[0].ID
		}
		for i := len(recent) - 1; i >= 0; i-- {
			snap.history = append(snap.history, llm.Turn{Question: recent[i].Question, Answer: recent[i].Answer})
		}
		return nil
	})
	return snap, err
}

func (s *Service) candidates(ctx context.Context, p domain.Profile) ([]domain.Ticket, error) {
	var out []domain.Ticket
	err := s.store.Read(ctx, func(q *store.Q) error {
		tickets, err := q.Tickets()
		if err != nil {
			return err
		}
		taken, err := q.Taken()
		out = domain.Candidates(tickets, taken, p)
		return err
	})
	return out, err
}

// modelProblem turns a model failure into a message the intern can act on.
func modelProblem(err error, msg string) error {
	switch {
	case errors.Is(err, llm.ErrModelAuth):
		msg = "Модель недоступна: OpenRouter не принял ключ. Администратору демо: проверьте LLM_API_KEY или запустите демо без ключа."
	case errors.Is(err, llm.ErrModelBusy):
		msg = "Модель сейчас перегружена запросами. Подождите минуту и повторите сообщение."
	case errors.Is(err, llm.ErrModelTimeout) || errors.Is(err, context.DeadlineExceeded):
		msg = "Модель не ответила вовремя. Повторите сообщение."
	case !errors.Is(err, llm.ErrModel):
		return err
	}
	return &Problem{Status: 502, Message: msg, Cause: err}
}

// Chat handles one intern message. Model calls run outside transactions; the
// write re-checks everything the model saw and stores the result atomically.
func (s *Service) Chat(ctx context.Context, user, message, requestID string) error {
	question, err := validate(message, requestID)
	if err != nil {
		return err
	}
	snap, err := s.read(ctx, user, requestID, question)
	if err != nil || snap.done {
		return err
	}

	restricted := snap.assignment != nil && snap.assignment.Status == StatusRestricted
	// A clarifying blocker means the bot asked «what stops you?»: this message is the answer.
	clarifying := !restricted && snap.blocker != nil && snap.blocker.Status == store.BlockerClarifying
	var intent llm.Intent
	if !restricted && !clarifying {
		intent, err = s.model.Classify(ctx, question, snap.assignment != nil, snap.history)
		if err != nil {
			return modelProblem(err, "Не удалось определить, что нужно сделать. Повторите сообщение.")
		}
	}
	// With a blocker already at the mentor, a model may read any question as the same blocker
	// from history. Answer about the task unless the message itself names an obstacle.
	if intent == llm.IntentBlocker && snap.blocker != nil && !domain.ReportsBlocker(question) {
		intent = llm.IntentTaskQuestion
	}
	if intent == llm.IntentPickTask && snap.assignment == nil {
		if snap.candidates, err = s.candidates(ctx, snap.profile); err != nil {
			return err
		}
	}

	mode := "system"
	reply := ""
	var selection *llm.Selection
	var plan blockerPlan
	pickEmpty := false
	instruction, hasInstruction := domain.Instruction(snap.docs)
	switch {
	case restricted:
		reply = MsgRestricted
	case clarifying:
		c := llm.BlockerCase{Assignment: s.modelAssignment(snap), Obstacle: snap.blocker.Obstacle, Answer: question}
		if hasInstruction {
			c.Check, c.Fixes = instruction.Check, instruction.Fixes
		}
		plan.assessment, err = s.model.AssessBlocker(ctx, c)
		if err != nil {
			return modelProblem(err, "Не удалось разобрать препятствие. Повторите сообщение.")
		}
		plan.resolve, plan.answer = true, question
		mode = s.model.Mode()
	case snap.assignment != nil && intent == llm.IntentBlocker && snap.blocker != nil:
		reply = openBlockerReply(snap.blocker)
	case snap.assignment != nil && intent == llm.IntentBlocker:
		plan.create = true
		var doc *domain.Doc
		if hasInstruction {
			doc = &instruction
		}
		reply = clarifyQuestion(snap.assignment.TicketID, question, doc)
	case snap.assignment != nil && intent == llm.IntentPickTask:
		a := snap.assignment
		reply = fmt.Sprintf("У тебя уже есть задача %s — «%s». Статус: «%s», срок — %s.\nСледующий шаг: %s",
			a.TicketID, a.Title, a.StatusName, a.DueLabel, a.NextStep)
	case snap.assignment != nil && intent == llm.IntentTaskQuestion:
		reply, err = s.model.Answer(ctx, question, s.modelAssignment(snap), snap.history)
		if err != nil {
			return modelProblem(err, "Не удалось получить ответ по задаче. Повторите сообщение.")
		}
		mode = s.model.Mode()
	case len(snap.candidates) > 0:
		sel, err := s.model.Select(ctx, question, snap.candidates, snap.profile, s.data.TeamGoal)
		if err != nil {
			return modelProblem(err, "Модель не вернула корректный выбор. Повторите запрос.")
		}
		selection = &sel
	case intent == llm.IntentPickTask:
		pickEmpty = true
	case snap.assignment != nil:
		reply = "Могу помочь с текущей задачей. Например, спроси, с чего начать."
	default:
		reply = "Могу подобрать стартовую задачу в Jira. Хочешь взять что-нибудь в работу?"
	}
	if s.AfterModel != nil {
		s.AfterModel()
	}

	err = s.store.Write(ctx, func(q *store.Q) error {
		if v, err := q.ChatVersion(user); err != nil || v != snap.version {
			if err != nil {
				return err
			}
			return problem(409, "Чат был сброшен. Отправьте сообщение снова.")
		}
		done, err := seen(q, user, requestID, question)
		if err != nil || done {
			return err
		}
		latest, err := q.LatestTurnID(user)
		if err != nil {
			return err
		}
		current, err := q.Assignment(user)
		if err != nil {
			return err
		}
		if latest != snap.latest || !sameAssignment(assignmentView(current, snap.profile, snap.today), snap.assignment) {
			return problem(409, "Чат или назначение обновились. Повторите запрос.")
		}
		if snap.assignment != nil && snap.assignment.Status != StatusRestricted {
			active, err := q.ActiveBlocker(user, snap.assignment.TicketID)
			if err != nil {
				return err
			}
			if !sameBlocker(active, snap.blocker) {
				return problem(409, "Блокер уже изменился. Повторите сообщение.")
			}
		}

		switch {
		case selection != nil:
			if reply, err = s.assign(q, snap, *selection); err != nil {
				return err
			}
			mode = s.model.Mode()
		case pickEmpty:
			if reply, err = s.reportNoMatch(q, snap.profile); err != nil {
				return err
			}
		case plan.create:
			helper, err := helperFor(q, snap.profile)
			if err != nil {
				return err
			}
			if err := q.InsertBlocker(user, snap.assignment.TicketID, helper.ID, question); err != nil {
				return err
			}
		case plan.resolve:
			if reply, err = s.resolveBlocker(q, snap, plan); err != nil {
				return err
			}
		}
		return q.InsertTurn(user, requestID, question, reply, mode)
	})
	if store.IsUniqueViolation(err) {
		return problem(409, "Задача больше недоступна. Повторите подбор.")
	}
	return err
}

func (s *Service) fresh(q *store.Q, p domain.Profile) ([]domain.Ticket, []domain.Ticket, error) {
	tickets, err := q.Tickets()
	if err != nil {
		return nil, nil, err
	}
	taken, err := q.Taken()
	if err != nil {
		return nil, nil, err
	}
	return tickets, domain.Candidates(tickets, taken, p), nil
}

func (s *Service) assign(q *store.Q, snap snapshot, sel llm.Selection) (string, error) {
	_, allowed, err := s.fresh(q, snap.profile)
	if err != nil {
		return "", err
	}
	idx := slices.IndexFunc(allowed, func(t domain.Ticket) bool { return t.ID == sel.TicketID })
	offered := slices.ContainsFunc(snap.candidates, func(t domain.Ticket) bool { return t.ID == sel.TicketID })
	if idx < 0 || !offered {
		return "", problem(409, "Задача больше недоступна. Повторите подбор.")
	}
	t := allowed[idx]
	today, _, err := s.today(q)
	if err != nil {
		return "", err
	}
	due := domain.DueDate(today, t.DueDays)
	if err := q.InsertAssignment(snap.profile.ID, t.ID, sel.Rationale, sel.FirstStep, s.model.Mode(), due); err != nil {
		return "", err
	}
	headline := fmt.Sprintf("Назначена %s — «%s».", t.ID, t.Title)
	deadline := fmt.Sprintf("Срок: %s. Статус в Jira: «%s».", due.Label(), domain.StatusName(domain.StatusAssigned))
	facts := fmt.Sprintf("Проверено бэкендом: команда «%s», грейд %s, метка starter, классификация internal, допуски: %s.",
		t.Team, snap.profile.Grade, grantsText(t.Requires))
	report := headline + "\nПочему: " + sel.Rationale + "\n" + facts + "\nСрок: " + due.Label() + "."
	if err := s.sendReport(q, snap.profile, "assigned", report); err != nil {
		return "", err
	}
	return headline + "\n" + sel.Rationale + "\nПервый шаг: " + sel.FirstStep + "\n" + deadline +
		" Когда начнёшь, нажми «Взять в работу» над чатом.", nil
}

func grantsText(grants []string) string {
	if len(grants) == 0 {
		return "не требуются"
	}
	return strings.Join(grants, ", ")
}

func (s *Service) reportNoMatch(q *store.Q, p domain.Profile) (string, error) {
	tickets, allowed, err := s.fresh(q, p)
	if err != nil {
		return "", err
	}
	if len(allowed) > 0 {
		return "", problem(409, "Список задач обновился. Повторите подбор.")
	}
	reply, report := MsgNoneForProfile, reportNone
	if domain.WhyNoMatch(tickets, p) == domain.ReasonTaken {
		reply, report = MsgAllTaken, reportTaken
	}
	return reply, s.sendReport(q, p, "no_match", report)
}

func (s *Service) sendReport(q *store.Q, intern domain.Profile, outcome, result string) error {
	lead, ok, err := q.Profile(intern.LeadID)
	if err != nil {
		return err
	}
	if !ok || lead.Role != domain.RoleLead {
		return problem(503, "Руководитель стажёра недоступен. Повторите запрос позже.")
	}
	heading := fmt.Sprintf("%s · стажёр %s · команда «%s»", intern.Name, intern.Grade, intern.Team)
	return q.InsertReport(lead.ID, intern.ID, outcome, heading+"\n"+result)
}

func sameBlocker(a, b *store.Blocker) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && a.Status == b.Status
}

// modelAssignment is the task context for the model: Jira facts and the instruction the intern may read.
func (s *Service) modelAssignment(snap snapshot) llm.Assignment {
	a := snap.assignment
	m := llm.Assignment{TicketID: a.TicketID, Title: a.Title, Rationale: a.Rationale, FirstStep: a.FirstStep,
		Status: a.StatusName, Due: a.DueLabel, NextStep: a.NextStep, Action: a.Action}
	if doc, ok := domain.Instruction(snap.docs); ok {
		m.Instruction, m.Guide = doc.Title, doc.Steps
	}
	return m
}
