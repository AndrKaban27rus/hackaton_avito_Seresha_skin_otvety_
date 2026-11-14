-- Demo state. jira_* tables are the local Jira simulation; with a real Jira
-- they are replaced by the Jira adapter and Jira becomes the source of truth.
CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY,
  role TEXT NOT NULL CHECK (role IN ('intern', 'lead')),
  name TEXT NOT NULL,
  team TEXT NOT NULL,
  grade TEXT NOT NULL DEFAULT '',
  grants TEXT[] NOT NULL DEFAULT '{}',
  lead_id TEXT,
  sort_order INT NOT NULL
);

CREATE TABLE IF NOT EXISTS jira_tickets (
  id TEXT PRIMARY KEY,
  team TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL,
  requires TEXT[],
  labels TEXT[] NOT NULL DEFAULT '{}',
  status TEXT NOT NULL,
  classification TEXT NOT NULL DEFAULT '',
  grades TEXT[],
  okr_priority INT NOT NULL DEFAULT 0,
  demo_rationale TEXT NOT NULL DEFAULT '',
  demo_first_step TEXT NOT NULL DEFAULT '',
  sort_order INT NOT NULL
);

-- One ticket per intern and one intern per ticket, even under concurrent writes.
CREATE TABLE IF NOT EXISTS jira_assignments (
  intern TEXT PRIMARY KEY REFERENCES profiles (id),
  ticket_id TEXT NOT NULL UNIQUE REFERENCES jira_tickets (id),
  status TEXT NOT NULL DEFAULT 'assigned',
  rationale TEXT NOT NULL,
  first_step TEXT NOT NULL,
  mode TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Stage 2: status workflow and deadline. Written to upgrade stage 1 databases in place.
ALTER TABLE jira_tickets ADD COLUMN IF NOT EXISTS due_days INT NOT NULL DEFAULT 3;
ALTER TABLE jira_assignments ADD COLUMN IF NOT EXISTS due_date DATE;
UPDATE jira_assignments SET due_date = ((created_at AT TIME ZONE 'UTC') + INTERVAL '3 hours')::date + 3
  WHERE due_date IS NULL;
ALTER TABLE jira_assignments ALTER COLUMN due_date SET NOT NULL;
ALTER TABLE jira_assignments ADD COLUMN IF NOT EXISTS status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE jira_assignments DROP CONSTRAINT IF EXISTS jira_assignments_status_check;
ALTER TABLE jira_assignments ADD CONSTRAINT jira_assignments_status_check
  CHECK (status IN ('assigned', 'in_progress', 'review'));

-- Demo clock: one row with a day offset shared by all profiles.
CREATE TABLE IF NOT EXISTS demo_clock (
  id SMALLINT PRIMARY KEY CHECK (id = 1),
  offset_days INT NOT NULL
);
INSERT INTO demo_clock (id, offset_days) VALUES (1, 0) ON CONFLICT (id) DO NOTHING;

-- Bot messages without a question: status confirmations and reminders.
-- dedup_key makes every message idempotent across retries and restarts.
CREATE TABLE IF NOT EXISTS bot_events (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES profiles (id),
  kind TEXT NOT NULL CHECK (kind IN ('status', 'due_soon', 'overdue')),
  dedup_key TEXT NOT NULL UNIQUE,
  text TEXT NOT NULL,
  hidden BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS chat_turns (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES profiles (id),
  request_id TEXT NOT NULL,
  question TEXT NOT NULL,
  answer TEXT NOT NULL,
  mode TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, request_id)
);

-- Reports to the lead: at most one per intern and outcome.
CREATE TABLE IF NOT EXISTS lead_inbox (
  id BIGSERIAL PRIMARY KEY,
  lead_id TEXT NOT NULL REFERENCES profiles (id),
  intern TEXT NOT NULL REFERENCES profiles (id),
  outcome TEXT NOT NULL CHECK (outcome IN ('assigned', 'no_match')),
  message TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (intern, outcome)
);

-- Bumped on every chat reset so a late model answer cannot restore old history.
CREATE TABLE IF NOT EXISTS chat_versions (
  user_id TEXT PRIMARY KEY REFERENCES profiles (id),
  version BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS inbox_resets (
  lead_id TEXT PRIMARY KEY REFERENCES profiles (id),
  last_report_id BIGINT NOT NULL
);

-- Demo sessions keyed by the SHA-256 of the cookie value.
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES profiles (id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Stage 3: mentor role, synthetic knowledge base and blockers.
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS mentor_id TEXT;
ALTER TABLE profiles DROP CONSTRAINT IF EXISTS profiles_role_check;
ALTER TABLE profiles ADD CONSTRAINT profiles_role_check CHECK (role IN ('intern', 'lead', 'mentor'));

-- Confluence, Grafana, Bitbucket and stand pages: synthetic test resources.
CREATE TABLE IF NOT EXISTS kb_docs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  ticket_id TEXT NOT NULL REFERENCES jira_tickets (id),
  title TEXT NOT NULL,
  requires TEXT[],
  body JSONB NOT NULL,
  sort_order INT NOT NULL
);

CREATE TABLE IF NOT EXISTS blockers (
  id BIGSERIAL PRIMARY KEY,
  intern TEXT NOT NULL REFERENCES profiles (id),
  ticket_id TEXT NOT NULL REFERENCES jira_tickets (id),
  helper_id TEXT NOT NULL REFERENCES profiles (id),
  status TEXT NOT NULL CHECK (status IN ('clarifying', 'open', 'self_help', 'closed')),
  obstacle TEXT NOT NULL,
  details TEXT NOT NULL DEFAULT '',
  request TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  opened_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ
);
-- At most one active blocker per intern and task: a repeat cannot notify twice.
CREATE UNIQUE INDEX IF NOT EXISTS blockers_one_active ON blockers (intern, ticket_id)
  WHERE status IN ('clarifying', 'open');

ALTER TABLE bot_events DROP CONSTRAINT IF EXISTS bot_events_kind_check;
ALTER TABLE bot_events ADD CONSTRAINT bot_events_kind_check
  CHECK (kind IN ('status', 'due_soon', 'overdue', 'blocker'));

-- Stage 4: frequent intern questions with answers approved by the team lead.
-- The bot replies with the stored answer as is; keywords serve for the mode without a model.
CREATE TABLE IF NOT EXISTS faq (
  id TEXT PRIMARY KEY,
  team TEXT NOT NULL,
  question TEXT NOT NULL CHECK (question <> ''),
  answer TEXT NOT NULL CHECK (answer <> ''),
  keywords TEXT[] NOT NULL DEFAULT '{}',
  sort_order INT NOT NULL
);
-- Short topic the bot lists when it did not understand a question.
ALTER TABLE faq ADD COLUMN IF NOT EXISTS topic TEXT NOT NULL DEFAULT '';
