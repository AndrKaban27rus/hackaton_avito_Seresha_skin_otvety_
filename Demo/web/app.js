const byId = (id) => document.getElementById(id);
const starterRequest = "Подбери мне стартовую задачу";
let currentState;
let conversation = "bot";
let pendingRequest;
let busy = false;

function element(tag, content, className) {
  const node = document.createElement(tag);
  if (content) node.textContent = content;
  if (className) node.className = className;
  return node;
}

async function api(path, payload) {
  const options = payload === undefined ? {} : {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify(payload),
  };
  const response = await fetch(path, options);
  const body = await response.json();
  if (!response.ok) {
    const error = new Error(body.error || "Запрос не выполнен. Попробуйте снова.");
    error.status = response.status;
    throw error;
  }
  return body;
}

async function run(work, pendingMessage = "") {
  if (busy) return;
  busy = true;
  const notice = byId("notice");
  const composerControls = byId("composer").querySelectorAll("button, textarea");
  const controls = [byId("profile"), byId("reset-chat"), byId("restart-pick"), ...composerControls];
  for (const control of controls) control.disabled = true;
  notice.textContent = pendingMessage;
  notice.className = "";
  try {
    await work();
    notice.textContent = "";
  } catch (error) {
    notice.textContent = error instanceof TypeError
      ? "Связь с сервером прервалась. Повторите запрос." : error.message;
    notice.className = "error";
  } finally {
    busy = false;
    for (const control of controls) control.disabled = false;
  }
}

function addMessage(author, text, {bot = false, timestamp, fixture = false} = {}) {
  const row = element("li", "", "message");
  const avatar = element("span", bot ? "✦" : author.charAt(0), bot ? "avatar bot" : "avatar");
  avatar.setAttribute("aria-hidden", "true");
  const body = element("div", "", "message-body");
  body.append(element("strong", author));
  if (timestamp) {
    body.append(element("small", new Date(timestamp).toLocaleTimeString("ru-RU", {
      hour: "2-digit", minute: "2-digit",
    })));
  }
  if (fixture) body.append(element("small", "Тестовый ответ"));
  body.append(element("p", text));
  row.append(avatar, body);
  byId("history").append(row);
}

function openConversation(id) {
  conversation = id;
  renderConversation();
}

function renderConversation() {
  const state = currentState;
  const lead = state.profile.role === "lead";
  const inBot = conversation === "bot";
  const channel = state.channels.find((item) => item.id === conversation);
  byId("bot-chat").setAttribute("aria-current", String(inBot));
  byId("composer").hidden = !inBot || lead;
  byId("reset-chat").hidden = !inBot || (lead ? state.notifications.length : state.chat.length) === 0;
  byId("restart-pick").hidden = !inBot || lead;
  byId("suggestion").hidden = Boolean(state.assignment);
  byId("mode").textContent = state.mode === "fixture" ? "Демо · тестовый подбор" : "Подбор через OpenRouter";

  const channels = byId("channels");
  channels.replaceChildren();
  for (const item of state.channels) {
    const button = element("button", "# " + item.name);
    button.type = "button";
    button.setAttribute("aria-current", String(item.id === conversation));
    button.onclick = () => openConversation(item.id);
    channels.append(button);
  }
  const badge = byId("notification-count");
  badge.textContent = state.notifications.length;
  badge.hidden = state.notifications.length === 0;
  byId("conversation-name").textContent = inBot ? "AI-buddy" : "# " + channel.name;
  byId("conversation-detail").textContent = inBot ? "Личные сообщения" : "Канал команды «Поиск»";
  byId("history").replaceChildren();

  if (channel) {
    for (const message of channel.messages) addMessage(message.author, message.text);
  } else if (lead) {
    if (!state.notifications.length) {
      byId("history").append(element("li", "Здесь появятся новые результаты подбора задач.", "empty-chat"));
    }
    for (const report of state.notifications) {
      addMessage("AI-buddy", report.message, {bot: true, timestamp: report.created_at});
    }
  } else {
    if (!state.chat.length) {
      const hint = state.assignment
        ? "Начни новый разговор о назначенной задаче."
        : "Напиши AI-buddy, чтобы подобрать стартовую задачу в Jira.";
      byId("history").append(element("li", hint, "empty-chat"));
    }
    for (const turn of state.chat) {
      addMessage(state.profile.name, turn.question, {timestamp: turn.created_at});
      addMessage("AI-buddy", turn.answer, {bot: true, fixture: turn.mode === "fixture"});
    }
  }

  const messages = byId("messages");
  messages.scrollTop = messages.scrollHeight;
}

function render(state) {
  currentState = state;
  const profiles = byId("profile");
  profiles.replaceChildren();
  for (const profile of state.profiles) {
    const label = profile.role === "lead" ? "Руководитель" : "Стажёр " + profile.grade;
    const option = element("option", profile.name + " · " + label);
    option.value = profile.id;
    profiles.append(option);
  }
  profiles.value = state.profile.id;
  renderConversation();
}

byId("bot-chat").onclick = () => openConversation("bot");
byId("profile").onchange = () => {
  const profile = byId("profile").value;
  run(async () => {
    const state = await api("/api/session", {profile});
    conversation = "bot";
    pendingRequest = undefined;
    byId("message").value = "";
    render(state);
  }).then(() => {
    if (currentState) byId("profile").value = currentState.profile.id;
  });
};

byId("suggestion").onclick = () => {
  byId("message").value = starterRequest;
  byId("message").focus();
};

byId("composer").onsubmit = (event) => {
  event.preventDefault();
  const input = byId("message");
  const message = input.value.trim();
  if (!message || busy) return;
  if (pendingRequest?.message !== message) {
    pendingRequest = {message, request_id: crypto.randomUUID()};
  }
  run(async () => {
    render(await api("/api/chat", pendingRequest));
    pendingRequest = undefined;
    input.value = "";
  }, "AI-buddy готовит ответ…").then(() => input.focus());
};

function resetConversation(path, pendingMessage) {
  return run(async () => {
    render(await api(path, {}));
    pendingRequest = undefined;
    byId("message").value = "";
  }, pendingMessage).then(() => {
    if (byId("composer").hidden) byId("bot-chat").focus();
    else byId("message").focus();
  });
}

byId("reset-chat").onclick = () => resetConversation("/api/chat/reset", "Сбрасываем переписку…");
byId("restart-pick").onclick = () => resetConversation("/api/pick/reset", "Сбрасываем подбор…");

run(async () => {
  try {
    render(await api("/api/state"));
  } catch (error) {
    if (error.status !== 401) throw error;
    render(await api("/api/session", {profile: "intern_01"}));
  }
});
