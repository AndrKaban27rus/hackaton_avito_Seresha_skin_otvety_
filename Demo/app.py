"""AI-buddy in a local Slack and Jira simulation."""
import json
import os
import re
import secrets
import sqlite3
from contextlib import contextmanager
from datetime import datetime, timezone
from http import cookies
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.error import URLError
from urllib.request import Request, urlopen
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parent
FIXTURE = json.loads((ROOT / "demo_data.json").read_text(encoding="utf-8"))
PROFILES = FIXTURE["profiles"]
TICKETS = {item["id"]: item for item in FIXTURE["tickets"]}
DATABASE = ROOT / "instance" / "slack-demo.sqlite3"
SESSIONS = {}
MODEL = "nvidia/nemotron-3.5-super-vl:free"
OPENROUTER_URL = "https://openrouter.ai/api/v1/chat/completions"
CHAT_CONTEXT_TURNS = 8


class Problem(Exception):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status


@contextmanager
def database(path):
    connection = sqlite3.connect(path)
    try:
        with connection:
            connection.row_factory = sqlite3.Row
            connection.execute("PRAGMA foreign_keys = ON")
            yield connection
    finally:
        connection.close()


NO_MATCH = "Сейчас нет подходящей задачи для твоего грейда и текущих допусков."
MODEL_ERRORS = (URLError, TimeoutError, ValueError, KeyError, TypeError, IndexError, AttributeError)


def initialize(path):
    path.parent.mkdir(parents=True, exist_ok=True)
    with database(path) as db:
        db.executescript("""
            CREATE TABLE IF NOT EXISTS jira_assignments (
                intern TEXT PRIMARY KEY, ticket_id TEXT NOT NULL UNIQUE,
                rationale TEXT NOT NULL, first_step TEXT NOT NULL, mode TEXT NOT NULL,
                created_at TEXT NOT NULL
            );
            CREATE TABLE IF NOT EXISTS slack_turns (
                id INTEGER PRIMARY KEY, user_id TEXT NOT NULL, request_id TEXT NOT NULL,
                question TEXT NOT NULL, answer TEXT NOT NULL, mode TEXT NOT NULL,
                created_at TEXT NOT NULL, UNIQUE(user_id, request_id)
            );
            CREATE TABLE IF NOT EXISTS slack_inbox (
                id INTEGER PRIMARY KEY, lead_id TEXT NOT NULL, intern TEXT NOT NULL,
                outcome TEXT NOT NULL CHECK(outcome IN ('assigned', 'no_match')),
                message TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(intern, outcome)
            );
            CREATE TABLE IF NOT EXISTS slack_chat_versions (
                user_id TEXT PRIMARY KEY, version INTEGER NOT NULL
            );
            CREATE TABLE IF NOT EXISTS slack_inbox_resets (
                lead_id TEXT PRIMARY KEY, last_report_id INTEGER NOT NULL
            );
        """)


def available_to(ticket, person):
    return (
        ticket.get("status") == "open"
        and ticket.get("classification") == "internal"
        and ticket.get("team") == person["team"]
        and "starter" in ticket.get("labels", [])
        and isinstance(ticket.get("grades"), list)
        and person["grade"] in ticket["grades"]
        and isinstance(ticket.get("requires"), list)
        and set(ticket["requires"]) <= set(person["grants"])
    )


def jira_candidates(db, person):
    assigned = {row["ticket_id"] for row in db.execute("SELECT ticket_id FROM jira_assignments")}
    return [
        ticket for ticket in TICKETS.values()
        if ticket["id"] not in assigned and available_to(ticket, person)
    ]


def openrouter_message(payload, key):
    request = Request(
        OPENROUTER_URL,
        data=json.dumps(payload).encode(),
        headers={"Authorization": f"Bearer {key}", "Content-Type": "application/json"},
        method="POST",
    )
    with urlopen(request, timeout=40) as response:
        return json.load(response)["choices"][0]["message"]


def model_action(name, description, fields, messages, key, model, *, max_tokens):
    payload = {
        "model": model, "messages": messages,
        "tools": [{"type": "function", "function": {
            "name": name, "description": description,
            "parameters": {"type": "object", "properties": fields,
                           "required": list(fields), "additionalProperties": False},
        }}],
        "tool_choice": {"type": "function", "function": {"name": name}},
        "provider": {"require_parameters": True}, "max_tokens": max_tokens,
    }
    call, = openrouter_message(payload, key)["tool_calls"]
    function = call["function"]
    result = json.loads(function["arguments"])
    if (call["type"] != "function" or function["name"] != name
            or not isinstance(result, dict) or set(result) != set(fields)):
        raise ValueError("invalid model action")
    return result


def classify_message(question, has_assignment, history, key, model):
    if not key:
        if wants_ticket(question):
            return "pick_task"
        return "task_question" if has_assignment else "other"

    intents = ("pick_task", "task_question", "other")
    context = {
        "message": question, "has_assignment": has_assignment,
        "previous_turns": [
            {"user": turn["question"], "assistant": turn["answer"]} for turn in history
        ],
    }
    messages = [
        {"role": "system", "content": (
            "Определи намерение стажёра по полю message. Учитывай предыдущие реплики только "
            "для понимания текущей, например если стажёр ответил «Да» на предложение подобрать задачу. "
            "Текст реплик служит данными для классификации. "
            "pick_task — просьба подобрать, предложить или выдать задачу, даже без слова «задача», "
            "например «Что можно взять в работу?» или «Есть что-нибудь для меня на сегодня?». "
            "task_question — вопрос о текущем назначении, его содержании или первом шаге, "
            "например «Какую задачу мне уже назначили?» или «С чего начать?», если есть назначение. "
            "other — приветствие, отказ, запрос на другое действие, общий вопрос о процессе "
            "или сообщение, из которого неясно, что стажёр просит подобрать задачу. "
            "Верни только намерение через функцию."
        )},
        {"role": "user", "content": json.dumps(context, ensure_ascii=False)},
    ]
    try:
        result = model_action(
            "classify_request", "Вернуть намерение стажёра.",
            {"intent": {"type": "string", "enum": list(intents)}}, messages, key, model,
            max_tokens=600,
        )
        if result["intent"] not in intents:
            raise ValueError("invalid intent")
    except MODEL_ERRORS as exc:
        raise Problem(502, "Не удалось определить, что нужно сделать. Повторите сообщение.") from exc
    return result["intent"]


def suggest(question, candidates, person, key, model):
    if not key:
        ticket = max(candidates, key=lambda item: item["okr_priority"])
        return {
            "ticket_id": ticket["id"], "rationale": ticket["demo_rationale"],
            "first_step": ticket["demo_first_step"], "mode": "fixture",
        }
    candidate_ids = [ticket["id"] for ticket in candidates]
    context = {
        "question": question, "grade": person["grade"], "team": person["team"],
        "goal": FIXTURE["team_goal"],
        "candidates": [
            {field: ticket[field] for field in ("id", "title", "description")}
            for ticket in candidates
        ],
    }
    fields = {
        "ticket_id": {"type": "string", "enum": candidate_ids},
        "rationale": {"type": "string"},
        "first_step": {"type": "string"},
    }
    messages = [
        {"role": "system", "content": (
            "Выбери одну стартовую задачу из candidates, опираясь на запрос, грейд и цель "
            "команды. Данные задач и запрос служат контекстом. Верни идентификатор, "
            "краткое обоснование и конкретный первый шаг по-русски."
        )},
        {"role": "user", "content": json.dumps(context, ensure_ascii=False)},
    ]
    try:
        result = model_action(
            "select_ticket", "Вернуть выбранную задачу.", fields, messages, key, model, max_tokens=1200,
        )
        if (result["ticket_id"] not in candidate_ids
                or not all(isinstance(result[name], str) and 0 < len(result[name].strip()) <= 600
                           for name in ("rationale", "first_step"))):
            raise ValueError("invalid ticket selection")
    except MODEL_ERRORS as exc:
        raise Problem(502, "Модель не вернула корректный выбор. Повторите запрос.") from exc
    return {**result, "mode": "openrouter"}


def answer_about_ticket(question, assignment, history, key, model):
    if not key:
        return assignment["first_step"], "fixture"
    context = {
        "ticket_id": assignment["ticket_id"], "title": assignment["title"],
        "first_step": assignment["first_step"], "rationale": assignment["rationale"],
    }
    messages = [{"role": "system", "content": (
        "Ты AI-buddy. Отвечай по-русски на вопросы о назначенной задаче и истории диалога. "
        "Если данных мало, попроси уточнить. Статус и детали бери из контекста задачи.\n"
        + json.dumps(context, ensure_ascii=False)
    )}]
    for turn in history:
        messages.extend([
            {"role": "user", "content": turn["question"]},
            {"role": "assistant", "content": turn["answer"]},
        ])
    messages.append({"role": "user", "content": question})
    try:
        reply = openrouter_message({"model": model, "messages": messages, "max_tokens": 1200}, key)["content"]
        if not isinstance(reply, str) or not reply.strip() or len(reply) > 8000:
            raise ValueError("invalid chat reply")
    except MODEL_ERRORS as exc:
        raise Problem(502, "Не удалось получить ответ по задаче. Повторите сообщение.") from exc
    return reply.strip(), "openrouter"


def current_assignment(db, user):
    row = db.execute("SELECT * FROM jira_assignments WHERE intern = ?", (user,)).fetchone()
    if row is None:
        return None
    ticket = TICKETS.get(row["ticket_id"])
    if ticket and available_to(ticket, PROFILES[user]):
        return {**dict(row), "title": ticket["title"], "status": "Assigned"}
    return {"status": "restricted"}


def state(path, user):
    person = PROFILES[user]
    with database(path) as db:
        db.execute("BEGIN")
        return {
            "profile": {field: person[field] for field in ("name", "role", "team", "grade")} | {"id": user},
            "profiles": [
                {"id": key, "name": item["name"], "role": item["role"], "grade": item["grade"]}
                for key, item in PROFILES.items()
            ],
            "channels": FIXTURE["channels"],
            "chat": [
                dict(row) for row in db.execute(
                    "SELECT question, answer, mode, created_at FROM slack_turns "
                    "WHERE user_id = ? ORDER BY id", (user,),
                )
            ],
            "notifications": [
                dict(row) for row in db.execute(
                    "SELECT message, created_at FROM slack_inbox WHERE lead_id = ? "
                    "AND id > COALESCE((SELECT last_report_id FROM slack_inbox_resets WHERE lead_id = ?), 0) "
                    "ORDER BY id", (user, user),
                )
            ],
            "assignment": current_assignment(db, user),
            "mode": "openrouter" if api_key() else "fixture",
        }


def validate_message(message):
    if not isinstance(message, str) or not message.strip() or len(message) > 1200:
        raise Problem(400, "Введите запрос длиной до 1200 символов.")
    return message.strip()


def wants_ticket(question):
    words = re.findall(r"[а-яё]+", question.lower())
    if re.search(r"\bчто (?:мне )?(?:можно )?взять в работу\b", " ".join(words)):
        return True
    return (
        any(word.startswith(("подбер", "подбир", "выбер", "выбир", "найд", "назнач", "дай", "дайте",
                             "предлож", "нуж", "хочу")) for word in words)
        and any(word.startswith(("задач", "задан", "тикет")) for word in words)
    )


def conversation_version(db, user):
    row = db.execute("SELECT version FROM slack_chat_versions WHERE user_id = ?", (user,)).fetchone()
    return row["version"] if row else 0


def reset_chat(path, user, *, restart_pick=False):
    with database(path) as db:
        db.execute("BEGIN IMMEDIATE")
        if PROFILES[user]["role"] == "lead":
            if restart_pick:
                raise Problem(403, "Начать подбор заново можно из профиля стажёра.")
            db.execute(
                "INSERT INTO slack_inbox_resets (lead_id, last_report_id) "
                "SELECT ?, COALESCE(MAX(id), 0) FROM slack_inbox WHERE lead_id = ? "
                "ON CONFLICT (lead_id) DO UPDATE SET "
                "last_report_id = MAX(last_report_id, excluded.last_report_id)",
                (user, user),
            )
            return
        if restart_pick:
            db.execute("DELETE FROM jira_assignments WHERE intern = ?", (user,))
            db.execute("DELETE FROM slack_inbox WHERE intern = ?", (user,))
        db.execute("DELETE FROM slack_turns WHERE user_id = ?", (user,))
        db.execute(
            "INSERT INTO slack_chat_versions VALUES (?, 1) "
            "ON CONFLICT (user_id) DO UPDATE SET version = version + 1", (user,),
        )


def existing_turn(db, user, request_id, question):
    row = db.execute(
        "SELECT question FROM slack_turns WHERE user_id = ? AND request_id = ?", (user, request_id),
    ).fetchone()
    if row and row["question"] != question:
        raise Problem(409, "Этот номер запроса уже использован. Отправьте сообщение снова.")
    return row is not None


def send_report(db, user, person, outcome, result, timestamp):
    lead = PROFILES.get(person["lead_id"])
    if not lead or lead["role"] != "lead":
        raise Problem(503, "Руководитель стажёра недоступен. Повторите запрос позже.")
    heading = f'{person["name"]} · стажёр {person["grade"]} · команда «{person["team"]}»'
    # A demo restart can remove the latest report while its ID is still stored in a reset cursor.
    db.execute(
        "INSERT INTO slack_inbox (id, lead_id, intern, outcome, message, created_at) VALUES ("
        "1 + MAX(COALESCE((SELECT MAX(id) FROM slack_inbox), 0), "
        "COALESCE((SELECT MAX(last_report_id) FROM slack_inbox_resets), 0)), ?, ?, ?, ?, ?) "
        "ON CONFLICT (intern, outcome) DO NOTHING",
        (person["lead_id"], user, outcome, f"{heading}\n{result}", timestamp),
    )


def chat(path, user, message, request_id):
    person = PROFILES[user]
    if person["role"] != "intern":
        raise Problem(403, "Подбор задачи доступен стажёру в личном чате с AI-buddy.")
    question = validate_message(message)
    if not isinstance(request_id, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,80}", request_id):
        raise Problem(400, "Некорректный номер запроса.")
    with database(path) as db:
        db.execute("BEGIN")
        if existing_turn(db, user, request_id, question):
            return
        version = conversation_version(db, user)
        assignment = current_assignment(db, user)
        history = db.execute(
            "SELECT id, question, answer FROM slack_turns WHERE user_id = ? ORDER BY id DESC LIMIT ?",
            (user, CHAT_CONTEXT_TURNS),
        ).fetchall()

    key = api_key()
    model = os.getenv("LLM_MODEL") or MODEL
    restricted = assignment and assignment["status"] == "restricted"
    intent = None if restricted else classify_message(question, bool(assignment), reversed(history), key, model)
    candidates = []
    if intent == "pick_task" and not assignment:
        with database(path) as db:
            candidates = jira_candidates(db, person)

    selection = None
    outcome = None
    mode = "system"
    if restricted:
        reply = "Детали назначенной задачи сейчас недоступны. Обратись к руководителю."
    elif assignment and intent == "pick_task":
        reply = f'У тебя уже есть задача {assignment["ticket_id"]} — «{assignment["title"]}».\n{assignment["first_step"]}'
    elif assignment and intent == "task_question":
        reply, mode = answer_about_ticket(question, assignment, reversed(history), key, model)
    elif candidates:
        selection = suggest(question, candidates, person, key, model)
    elif intent == "pick_task":
        reply = NO_MATCH
        outcome = "no_match"
    else:
        reply = "Могу помочь с текущей задачей. Например, спроси, с чего начать." if assignment else (
            "Могу подобрать стартовую задачу в Jira. Хочешь взять что-нибудь в работу?"
        )

    timestamp = datetime.now(timezone.utc).isoformat(timespec="seconds")
    with database(path) as db:
        db.execute("BEGIN IMMEDIATE")
        if conversation_version(db, user) != version:
            raise Problem(409, "Чат был сброшен. Отправьте сообщение снова.")
        if existing_turn(db, user, request_id, question):
            return
        latest = db.execute("SELECT MAX(id) FROM slack_turns WHERE user_id = ?", (user,)).fetchone()[0]
        if latest != (history[0]["id"] if history else None) or current_assignment(db, user) != assignment:
            raise Problem(409, "Чат или назначение обновились. Повторите запрос.")

        if selection:
            allowed = {ticket["id"]: ticket for ticket in jira_candidates(db, person)}
            ticket = allowed.get(selection["ticket_id"])
            if not ticket or selection["ticket_id"] not in {item["id"] for item in candidates}:
                raise Problem(409, "Задача больше недоступна. Повторите подбор.")
            mode = selection["mode"]
            db.execute(
                "INSERT INTO jira_assignments VALUES (?, ?, ?, ?, ?, ?)",
                (user, ticket["id"], selection["rationale"], selection["first_step"], mode, timestamp),
            )
            report = f'Назначена {ticket["id"]} — «{ticket["title"]}».'
            reply = f'{report}\n{selection["rationale"]}\nПервый шаг: {selection["first_step"]}'
            send_report(db, user, person, "assigned", report, timestamp)
        elif outcome == "no_match":
            if jira_candidates(db, person):
                raise Problem(409, "Список задач обновился. Повторите подбор.")
            send_report(db, user, person, outcome,
                        "Подходящая задача не найдена. Среди открытых задач нет доступных для E1 "
                        "с текущими допусками.", timestamp)

        db.execute(
            "INSERT INTO slack_turns (user_id, request_id, question, answer, mode, created_at) "
            "VALUES (?, ?, ?, ?, ?, ?)", (user, request_id, question, reply, mode, timestamp),
        )


def api_key():
    return os.getenv("LLM_API_KEY", "").strip()


class Handler(BaseHTTPRequestHandler):
    db_path = DATABASE

    def send_data(self, status, data, mime="application/json; charset=utf-8", session=None):
        body = data if isinstance(data, bytes) else json.dumps(data, ensure_ascii=False).encode()
        self.send_response(status)
        self.send_header("Content-Type", mime)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        if session:
            self.send_header("Set-Cookie", f"demo_session={session}; Path=/; HttpOnly; SameSite=Strict")
        self.end_headers()
        self.wfile.write(body)

    def read_user(self):
        jar = cookies.SimpleCookie(self.headers.get("Cookie", ""))
        session = jar.get("demo_session")
        user = SESSIONS.get(session.value) if session else None
        if user is None:
            raise Problem(401, "Выберите тестовый профиль.")
        return user

    def do_GET(self):
        path = urlsplit(self.path).path
        if path in ("/", "/style.css", "/app.js"):
            files = {"/": ("index.html", "text/html"),
                     "/style.css": ("style.css", "text/css"),
                     "/app.js": ("app.js", "text/javascript")}
            name, mime = files[path]
            self.send_data(200, (ROOT / "web" / name).read_bytes(), mime + "; charset=utf-8")
            return
        try:
            if path != "/api/state":
                raise Problem(404, "Страница не найдена.")
            self.send_data(200, state(self.db_path, self.read_user()))
        except Problem as exc:
            self.send_data(exc.status, {"error": str(exc)})
        except sqlite3.Error:
            self.send_data(503, {"error": "Тестовый сервис временно недоступен. Повторите запрос."})

    def do_POST(self):
        try:
            origin = self.headers.get("Origin")
            if origin and urlsplit(origin).netloc != self.headers.get("Host"):
                raise Problem(403, "Источник запроса не разрешён.")
            if self.headers.get_content_type() != "application/json":
                raise Problem(415, "Ожидается application/json.")
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 8192:
                raise Problem(400, "Недопустимый размер запроса.")
            body = json.loads(self.rfile.read(size))
            if not isinstance(body, dict):
                raise Problem(400, "Ожидается JSON-объект.")
            path = urlsplit(self.path).path
            if path == "/api/session":
                login = body.get("profile")
                if not isinstance(login, str) or login not in PROFILES:
                    raise Problem(400, "Неизвестный тестовый профиль.")
                jar = cookies.SimpleCookie(self.headers.get("Cookie", ""))
                old = jar.get("demo_session")
                if old:
                    SESSIONS.pop(old.value, None)
                session = secrets.token_urlsafe(24)
                SESSIONS[session] = login
                self.send_data(200, state(self.db_path, login), session=session)
                return
            user = self.read_user()
            if path == "/api/chat":
                chat(self.db_path, user, body.get("message"), body.get("request_id"))
            elif path == "/api/chat/reset":
                reset_chat(self.db_path, user)
            elif path == "/api/pick/reset":
                reset_chat(self.db_path, user, restart_pick=True)
            else:
                raise Problem(404, "Действие не найдено.")
            self.send_data(200, state(self.db_path, user))
        except (ValueError, UnicodeError):
            self.send_data(400, {"error": "Некорректный JSON."})
        except Problem as exc:
            self.send_data(exc.status, {"error": str(exc)})
        except sqlite3.Error:
            self.send_data(503, {"error": "Тестовый сервис временно недоступен. Повторите запрос."})


def main():
    Handler.db_path = Path(os.getenv("AI_BUDDY_DB") or DATABASE)
    initialize(Handler.db_path)
    port = int(os.getenv("AI_BUDDY_PORT", "8765"))
    with ThreadingHTTPServer(("127.0.0.1", port), Handler) as server:
        print(f"AI-buddy: http://127.0.0.1:{port}", flush=True)
        server.serve_forever()


if __name__ == "__main__":
    main()
