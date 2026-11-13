import io
import json
import sqlite3
import tempfile
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from http.client import HTTPConnection
from pathlib import Path
from unittest.mock import patch
from urllib.error import URLError

import app


class HttpTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.path = Path(self.folder.name) / "test.sqlite3"
        app.initialize(self.path)
        handler = type("TestHandler", (app.Handler,), {
            "db_path": self.path, "log_message": lambda *_: None,
        })
        self.server = app.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.stop_server)
        self.addCleanup(app.SESSIONS.clear)
        key = patch.object(app, "api_key", return_value="")
        key.start()
        self.addCleanup(key.stop)

    def stop_server(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)

    def request(self, path, data=None, session="", headers=None):
        conn = HTTPConnection("127.0.0.1", self.server.server_port, timeout=5)
        try:
            request_headers = {"Cookie": session, **(headers or {})}
            if data is not None:
                request_headers.setdefault("Content-Type", "application/json")
            conn.request("POST" if data is not None else "GET", path,
                         json.dumps(data) if data is not None else None, request_headers)
            response = conn.getresponse()
            raw = response.read()
            result = json.loads(raw) if response.getheader("Content-Type").startswith("application/json") else raw
            cookie = (response.getheader("Set-Cookie") or "").split(";", 1)[0]
            return response.status, result, cookie
        finally:
            conn.close()

    def login(self, name):
        status, _, cookie = self.request("/api/session", {"profile": name})
        self.assertEqual(status, 200)
        self.assertTrue(cookie)
        return cookie

    def send(self, session, message="Подбери мне стартовую задачу", request_id="request-1"):
        return self.request("/api/chat", {"message": message, "request_id": request_id}, session)

    def state(self, session):
        status, state, _ = self.request("/api/state", session=session)
        self.assertEqual(status, 200)
        return state

    def test_assignment_followup_and_chat_reset(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        status, state, _ = self.send(intern)
        self.assertEqual(status, 200)
        self.assertEqual(state["assignment"]["ticket_id"], "BACK-112")
        self.assertEqual(state["assignment"]["status"], "Assigned")
        self.assertIn("Первый шаг:", state["chat"][0]["answer"])
        self.assertEqual(state["chat"][0]["mode"], "fixture")

        self.assertEqual(self.send(intern)[1]["chat"], state["chat"])
        self.assertIn("уже есть", self.send(intern, request_id="retry")[1]["chat"][-1]["answer"])
        followup = self.send(intern, "С чего начать?", "followup")[1]
        self.assertEqual(followup["chat"][-1]["answer"], state["assignment"]["first_step"])
        report, = self.state(lead)["notifications"]
        self.assertIn("Алексей Сергеевич Ким · стажёр E1 · команда «Поиск»", report["message"])
        self.assertIn("Назначена BACK-112", report["message"])
        self.assertEqual(self.state(lead)["chat"], [])
        self.assertEqual(self.state(intern)["notifications"], [])
        self.assertEqual(self.state(self.login("intern_01"))["chat"], followup["chat"])

        status, reset, _ = self.request("/api/chat/reset", {}, intern)
        self.assertEqual(status, 200)
        self.assertEqual(reset["chat"], [])
        self.assertEqual(reset["assignment"], state["assignment"])
        self.assertEqual(self.state(lead)["notifications"], [report])

    def test_lead_reset_hides_old_reports_and_shows_new_ones(self):
        first = self.login("intern_01")
        second = self.login("intern_02")
        lead = self.login("lead_01")
        self.assertEqual(self.send(first)[0], 200)
        self.assertEqual(len(self.state(lead)["notifications"]), 1)

        status, reset, _ = self.request("/api/chat/reset", {}, lead)
        self.assertEqual(status, 200)
        self.assertEqual(reset["notifications"], [])
        self.assertEqual(self.state(self.login("lead_01"))["notifications"], [])
        self.assertEqual(self.state(first)["assignment"]["ticket_id"], "BACK-112")
        self.assertEqual(len(self.state(first)["chat"]), 1)
        self.assertEqual(self.send(first, request_id="repeat")[0], 200)
        self.assertEqual(self.state(lead)["notifications"], [])

        self.assertEqual(self.send(second)[1]["assignment"]["ticket_id"], "BACK-118")
        report, = self.state(lead)["notifications"]
        self.assertIn("Елена Игоревна Орлова", report["message"])

    def test_restart_pick_releases_assignment_and_can_notify_again(self):
        intern = self.login("intern_01")
        second = self.login("intern_02")
        lead = self.login("lead_01")
        self.assertEqual(self.send(second)[1]["assignment"]["ticket_id"], "BACK-112")
        self.assertEqual(self.send(intern)[1]["assignment"]["ticket_id"], "BACK-118")
        self.assertEqual(self.request("/api/pick/reset", {}, lead)[0], 403)
        self.assertEqual(self.request("/api/chat/reset", {}, lead)[1]["notifications"], [])

        status, state, _ = self.request("/api/pick/reset", {}, intern)
        self.assertEqual(status, 200)
        self.assertIsNone(state["assignment"])
        self.assertEqual(state["chat"], [])
        with app.database(self.path) as db:
            reports = db.execute("SELECT intern FROM slack_inbox").fetchall()
        self.assertEqual([row["intern"] for row in reports], ["intern_02"])

        status, state, _ = self.send(intern, "Что можно взять в работу?", "new-pick")
        self.assertEqual(status, 200)
        self.assertEqual(state["assignment"]["ticket_id"], "BACK-118")
        self.assertIn("Назначена BACK-118", state["chat"][0]["answer"])
        report, = self.state(lead)["notifications"]
        self.assertIn("Назначена BACK-118", report["message"])

    def test_no_candidates_reports_once_when_both_tasks_are_taken(self):
        first = self.login("intern_02")
        second = self.login("intern_01")
        intern = self.login("intern_03")
        lead = self.login("lead_01")
        self.assertEqual(self.send(first)[1]["assignment"]["ticket_id"], "BACK-112")
        self.assertEqual(self.send(second)[1]["assignment"]["ticket_id"], "BACK-118")
        with patch.object(app, "suggest") as model:
            status, state, _ = self.send(intern)
            self.assertEqual(status, 200)
            self.assertIsNone(state["assignment"])
            self.assertEqual(state["chat"][0]["answer"], app.NO_MATCH)
            self.assertEqual(self.send(intern, request_id="repeat")[0], 200)
            model.assert_not_called()
        reports = self.state(lead)["notifications"]
        self.assertEqual(len(reports), 3)
        report = reports[-1]
        self.assertIn("Илья Андреевич Соколов · стажёр E1 · команда «Поиск»", report["message"])
        self.assertIn("Подходящая задача не найдена", report["message"])
        for hidden in ("BACK-104", "BACK-121", "BACK-130", "profiles_ki"):
            self.assertNotIn(hidden, json.dumps([state, report], ensure_ascii=False))

    def test_model_routes_natural_requests_before_searching_jira(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        question = "Есть что-нибудь для меня на сегодня?"
        routes = iter(("other", "pick_task", "task_question"))
        contexts = []

        def response(payload, key):
            tool = payload.get("tool_choice", {}).get("function", {}).get("name")
            if tool is None:
                self.assertEqual(payload["messages"][-1]["content"], "С чего начать?")
                return {"content": "Открой тестовый стенд."}
            context = json.loads(payload["messages"][-1]["content"])
            contexts.append((tool, context))
            if tool == "classify_request":
                result = {"intent": next(routes)}
            else:
                self.assertEqual(tool, "select_ticket")
                self.assertEqual([ticket["id"] for ticket in context["candidates"]], ["BACK-112", "BACK-118"])
                result = {"ticket_id": "BACK-118", "rationale": "Подходит.", "first_step": "Открой стенд."}
            return {"tool_calls": [{"type": "function", "function": {
                "name": tool, "arguments": json.dumps(result),
            }}]}

        with patch.object(app, "api_key", return_value="test"), patch.object(
                app, "openrouter_message", side_effect=response), patch.object(
                app, "jira_candidates", wraps=app.jira_candidates) as search:
            status, state, _ = self.send(intern, "Привет")
            self.assertEqual(status, 200)
            self.assertIsNone(state["assignment"])
            search.assert_not_called()
            previous_reply = state["chat"][-1]["answer"]

            status, state, _ = self.send(intern, question, "pick")
            self.assertEqual(status, 200)
            self.assertEqual(state["assignment"]["ticket_id"], "BACK-118")
            self.assertEqual(search.call_count, 2)
            self.assertEqual(len(self.state(lead)["notifications"]), 1)
            self.assertEqual(contexts[1], ("classify_request", {
                "message": question, "has_assignment": False,
                "previous_turns": [{"user": "Привет", "assistant": previous_reply}],
            }))
            self.assertEqual(contexts[2][0], "select_ticket")

            status, state, _ = self.send(intern, "С чего начать?", "followup")
            self.assertEqual(status, 200)
            self.assertEqual(state["chat"][-1]["answer"], "Открой тестовый стенд.")
            self.assertEqual(search.call_count, 2)

    def test_invalid_model_intent_leaves_state_empty(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        reply = {"tool_calls": [{"type": "function", "function": {
            "name": "classify_request", "arguments": '{"intent":"assign_any_ticket"}',
        }}]}
        with patch.object(app, "api_key", return_value="test"), patch.object(
                app, "openrouter_message", return_value=reply), patch.object(app, "jira_candidates") as search:
            self.assertEqual(self.send(intern)[0], 502)
            search.assert_not_called()
        self.assertEqual(self.state(intern)["chat"], [])
        self.assertIsNone(self.state(intern)["assignment"])
        self.assertEqual(self.state(lead)["notifications"], [])

    def test_model_and_jira_errors_do_not_create_results(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        with patch.object(app, "api_key", return_value="test"), patch.object(
                app, "urlopen", side_effect=URLError("test")):
            self.assertEqual(self.send(intern)[0], 502)
        with app.database(self.path) as db:
            db.execute("""
                CREATE TRIGGER jira_unavailable BEFORE INSERT ON jira_assignments
                BEGIN SELECT RAISE(ABORT, 'Jira unavailable'); END
            """)
        status, error, _ = self.send(intern)
        self.assertEqual(status, 503)
        self.assertIn("Повторите запрос", error["error"])
        self.assertIsNone(self.state(intern)["assignment"])
        self.assertEqual(self.state(intern)["chat"], [])
        self.assertEqual(self.state(lead)["notifications"], [])
        with app.database(self.path) as db:
            db.execute("DROP TRIGGER jira_unavailable")
        self.assertEqual(self.send(intern)[0], 200)

    def test_concurrent_retry_does_not_duplicate_assignment(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        suggest = app.suggest
        barrier = threading.Barrier(2, timeout=3)

        def concurrent_suggest(*args):
            barrier.wait()
            return suggest(*args)

        with patch.object(app, "suggest", side_effect=concurrent_suggest), ThreadPoolExecutor(2) as pool:
            requests = [pool.submit(self.send, intern) for _ in range(2)]
            self.assertEqual([job.result()[0] for job in requests], [200, 200])

        self.assertEqual(len(self.state(intern)["chat"]), 1)
        self.assertEqual(len(self.state(lead)["notifications"]), 1)

    def test_rechecks_availability_at_assignment(self):
        intern = self.login("intern_01")
        lead = self.login("lead_01")
        ticket = app.TICKETS["BACK-112"]
        old_status = ticket["status"]
        self.addCleanup(ticket.__setitem__, "status", old_status)
        suggest = app.suggest

        def close_after_selection(*args):
            selected = suggest(*args)
            ticket["status"] = "closed"
            return selected

        with patch.object(app, "suggest", side_effect=close_after_selection):
            self.assertEqual(self.send(intern)[0], 409)
        self.assertIsNone(self.state(intern)["assignment"])
        self.assertEqual(self.state(lead)["notifications"], [])

    def test_session_validation(self):
        self.assertEqual(self.request("/")[0], 200)
        self.assertEqual(self.request("/api/state")[0], 401)
        lead = self.login("lead_01")
        self.assertEqual(self.send(lead)[0], 403)
        intern = self.login("intern_01")
        self.assertEqual(self.send(intern, request_id="bad id")[0], 400)
        self.assertEqual(self.request("/api/chat", {}, intern, {"Origin": "http://example.test"})[0], 403)


class ModelTests(unittest.TestCase):
    def test_model_gets_only_allowed_tasks_and_cannot_select_excluded_id(self):
        with app.database(":memory:") as db:
            db.execute("CREATE TABLE jira_assignments (ticket_id TEXT)")
            person = app.PROFILES["intern_01"]
            candidates = app.jira_candidates(db, person)
        result = {"ticket_id": "BACK-118", "rationale": "Начни с тестов.", "first_step": "Открой тесты."}

        def response(request, timeout):
            payload = json.loads(request.data)
            context = json.loads(payload["messages"][1]["content"])
            self.assertEqual([ticket["id"] for ticket in context["candidates"]], ["BACK-112", "BACK-118"])
            self.assertNotIn("Алексей", payload["messages"][1]["content"])
            for excluded in ("BACK-104", "BACK-121", "BACK-130", "profiles_ki"):
                self.assertNotIn(excluded, payload["messages"][1]["content"])
            return io.BytesIO(json.dumps({"choices": [{"message": {"tool_calls": [{
                "type": "function", "function": {"name": "select_ticket", "arguments": json.dumps(result)},
            }]}}]}).encode())

        with patch.object(app, "urlopen", side_effect=response):
            choice = app.suggest("Выбери задачу", candidates, person, "test", "test-model")
            self.assertEqual(choice["ticket_id"], "BACK-118")
            result["ticket_id"] = "BACK-104"
            with self.assertRaises(app.Problem) as error:
                app.suggest("Выбери задачу", candidates, person, "test", "test-model")
            self.assertEqual(error.exception.status, 502)

    def test_followup_keeps_assignment_and_history(self):
        assignment = {"ticket_id": "BACK-112", "title": "Кэш", "rationale": "Ускорение",
                      "first_step": "Сделай базовый замер."}
        history = [{"question": "С чего начать?", "answer": "Сделай базовый замер."}]
        with patch.object(app, "openrouter_message", return_value={"content": "Измерь p95."}) as call:
            answer, mode = app.answer_about_ticket("Что измерять?", assignment, history, "test", "model")
        self.assertEqual((answer, mode), ("Измерь p95.", "openrouter"))
        payload = call.call_args.args[0]
        self.assertIn("BACK-112", payload["messages"][0]["content"])
        self.assertEqual([item["content"] for item in payload["messages"][1:]],
                         ["С чего начать?", "Сделай базовый замер.", "Что измерять?"])
