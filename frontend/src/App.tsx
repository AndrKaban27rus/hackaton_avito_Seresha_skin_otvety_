import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  api,
  ApiError,
  dueHint,
  EVENT_LABELS,
  formatTime,
  linkParts,
  loadState,
  NETWORK_ERROR,
  profileLabel,
  type State,
  TURN_LABELS,
} from './api';

const STARTER_REQUEST = 'Подбери мне стартовую задачу';
const BOT = 'bot';

type Pending = { message: string; request_id: string };
type Notice = { text: string; error: boolean } | null;

function Message({ author, text, bot, time, label }: {
  author: string;
  text: string;
  bot?: boolean;
  time?: string;
  label?: string;
}) {
  return (
    <li className="message">
      <span className={bot ? 'avatar bot' : 'avatar'} aria-hidden="true">
        {bot ? '✦' : author.charAt(0)}
      </span>
      <div className="message-body">
        <strong>{author}</strong>
        {time && <small>{formatTime(time)}</small>}
        {label && <small className="tag">{label}</small>}
        <p>
          {linkParts(text).map((part, i) =>
            part.href ? (
              <a key={i} href={part.href} target="_blank" rel="noreferrer">
                {part.text}
              </a>
            ) : (
              <Fragment key={i}>{part.text}</Fragment>
            ),
          )}
        </p>
      </div>
    </li>
  );
}

type Item = { at: string; key: string; node: ReactNode };

/** Intern chat: question-answer turns and bot messages, ordered by time. */
function internItems(state: State): Item[] {
  const items: Item[] = [];
  state.chat.forEach((turn, i) => {
    items.push({
      at: turn.created_at,
      key: `t${i}`,
      node: (
        <>
          <Message author={state.profile.name} text={turn.question} time={turn.created_at} />
          <Message
            author="AI-buddy"
            text={turn.answer}
            bot
            label={TURN_LABELS[turn.mode]}
          />
        </>
      ),
    });
  });
  state.events.forEach((event, i) => {
    items.push({
      at: event.created_at,
      key: `e${i}`,
      node: <Message author="AI-buddy" text={event.text} bot time={event.created_at} label={EVENT_LABELS[event.kind]} />,
    });
  });
  return items.sort((a, b) => a.at.localeCompare(b.at));
}

function History({ state, conversation }: { state: State; conversation: string }) {
  const channel = state.channels.find((c) => c.id === conversation);
  if (channel) {
    return channel.messages.map((m, i) => <Message key={i} author={m.author} text={m.text} />);
  }
  if (state.profile.role === 'mentor') {
    if (state.help_requests.length) return null;
    return <li className="empty-chat">Здесь появятся запросы о помощи от стажёров.</li>;
  }
  if (state.profile.role === 'lead') {
    if (!state.notifications.length) {
      return <li className="empty-chat">Здесь появятся новые результаты подбора задач.</li>;
    }
    return state.notifications.map((r, i) => (
      <Message key={i} author="AI-buddy" text={r.message} bot time={r.created_at} />
    ));
  }
  const items = internItems(state);
  if (!items.length) {
    return (
      <li className="empty-chat">
        {state.assignment
          ? 'Начни новый разговор о назначенной задаче.'
          : 'Напиши AI-buddy, чтобы подобрать стартовую задачу в Jira.'}
      </li>
    );
  }
  return items.map((item) => <Fragment key={item.key}>{item.node}</Fragment>);
}

function BlockerNote({ state, busy, onClose }: { state: State; busy: boolean; onClose: (id: number) => void }) {
  const b = state.blocker;
  if (!b) return null;
  if (b.status === 'clarifying') {
    return <p className="blocker">AI-buddy ждёт уточнения: что мешает по «{b.obstacle}»?</p>;
  }
  return (
    <div className="blocker open">
      <p>
        Блокер у наставника: {b.helper_name}. Препятствие: «{b.obstacle}».
      </p>
      <button type="button" disabled={busy} onClick={() => onClose(b.id)}>
        Блокер снят
      </button>
    </div>
  );
}

function HelpRequests({ state, busy, onClose }: { state: State; busy: boolean; onClose: (id: number) => void }) {
  if (state.profile.role === 'intern' || !state.help_requests.length) return null;
  return (
    <section className="help" aria-label="Запросы о помощи">
      <h2>Запросы о помощи</h2>
      {state.help_requests.map((b) => (
        <article key={b.id} className="help-request">
          <p>{b.message}</p>
          <small>{formatTime(b.created_at)}</small>
          <button type="button" disabled={busy} onClick={() => onClose(b.id)}>
            Помог, снять блокер
          </button>
        </article>
      ))}
    </section>
  );
}

function AssignmentPanel({ state, busy, onMove, onClose }: {
  state: State;
  busy: boolean;
  onMove: (ticket: string, status: string) => void;
  onClose: (id: number) => void;
}) {
  const a = state.assignment;
  if (state.profile.role !== 'intern' || !a) return null;
  if (!a.ticket_id) {
    return (
      <section className="assignment" aria-label="Текущая задача в Jira">
        <span className="status">{a.status_name}</span> Детали задачи скрыты
      </section>
    );
  }
  return (
    <section className="assignment" aria-label="Текущая задача в Jira">
      <div className="assignment-head">
        <span className={`status status-${a.status}`}>{a.status_name}</span>
        <strong>
          {a.ticket_id} · {a.title}
        </strong>
      </div>
      <p className={a.overdue ? 'due overdue' : 'due'}>
        Срок: {a.due_label} · {dueHint(a)}
      </p>
      <p className="next">Следующий шаг: {a.next_step}</p>
      {a.resources && a.resources.length > 0 && (
        <div className="resources">
          <span>Инструкция и инструменты (тестовые):</span>
          <ul>
            {a.resources.map((r) => (
              <li key={r.id}>
                <a href={r.url} target="_blank" rel="noreferrer">
                  {r.title}
                </a>
              </li>
            ))}
          </ul>
        </div>
      )}
      <BlockerNote state={state} busy={busy} onClose={onClose} />
      {a.action && a.next_status && (
        <button type="button" className="primary" disabled={busy} onClick={() => onMove(a.ticket_id!, a.next_status!)}>
          {a.action}
        </button>
      )}
    </section>
  );
}

function TeamPanel({ state }: { state: State }) {
  if (state.profile.role !== 'lead' || !state.team.length) return null;
  return (
    <section className="team" aria-label="Задачи стажёров">
      <h2>Задачи стажёров</h2>
      <ul>
        {state.team.map((row) => (
          <li key={row.intern_id}>
            <span className="team-name">{row.name}</span>
            <span className="team-task">{row.ticket_id ? `${row.ticket_id} · ${row.title}` : 'Задача не назначена'}</span>
            <span className={`status status-${row.status}`}>{row.status_name}</span>
            <span className={row.overdue ? 'due overdue' : 'due'}>
              {row.due_label ? `${row.overdue ? 'Просрочено: ' : 'Срок: '}${row.due_label}` : ''}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
}

export function App() {
  const client = useQueryClient();
  const query = useQuery({ queryKey: ['state'], queryFn: loadState, retry: false });
  const state = query.data;
  const [conversation, setConversation] = useState(BOT);
  const [draft, setDraft] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const pending = useRef<Pending | null>(null);
  const messages = useRef<HTMLElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (messages.current) messages.current.scrollTop = messages.current.scrollHeight;
  }, [state, conversation]);

  async function run(work: () => Promise<State>, wait: string, after?: () => void) {
    if (busy) return;
    setBusy(true);
    setNotice(wait ? { text: wait, error: false } : null);
    try {
      client.setQueryData(['state'], await work());
      setNotice(null);
      after?.();
    } catch (error) {
      setNotice({ text: error instanceof ApiError ? error.message : NETWORK_ERROR, error: true });
    } finally {
      setBusy(false);
    }
  }

  if (!state) {
    const failed = query.error instanceof ApiError ? query.error.message : query.error ? NETWORK_ERROR : '';
    return (
      <main className="loading" role="status">
        {failed || 'Загружаем рабочее пространство…'}
      </main>
    );
  }

  // Leads and mentors read reports and help requests; only interns write to the bot.
  const lead = state.profile.role !== 'intern';
  const inBot = conversation === BOT;
  const channel = state.channels.find((c) => c.id === conversation);
  const chatLength = lead ? state.notifications.length : state.chat.length + state.events.length;
  const resetVisible = inBot && chatLength > 0;
  const unread = state.notifications.length + state.help_requests.length;

  const switchProfile = (profile: string) =>
    run(() => api('/api/session', { profile }), '', () => {
      setConversation(BOT);
      setDraft('');
      pending.current = null;
    });

  const send = (event: { preventDefault(): void }) => {
    event.preventDefault();
    const message = draft.trim();
    if (!message || busy) return;
    // The same text after a failure reuses its request ID, so the server answers it once.
    if (pending.current?.message !== message) pending.current = { message, request_id: crypto.randomUUID() };
    const request = pending.current;
    void run(() => api('/api/chat', request), 'AI-buddy готовит ответ…', () => {
      pending.current = null;
      setDraft('');
      input.current?.focus();
    });
  };

  const reset = (path: string, wait: string) =>
    run(() => api(path, {}), wait, () => {
      pending.current = null;
      setDraft('');
    });

  const move = (ticket_id: string, status: string) =>
    void run(() => api('/api/task/status', { ticket_id, status }), 'Записываем статус в Jira…');

  const closeBlocker = (blocker_id: number) =>
    void run(() => api('/api/blocker/close', { blocker_id }), 'Снимаем блокер…');

  const clock = (action: 'next_day' | 'reset') =>
    void run(() => api('/api/clock', { action }), action === 'next_day' ? 'Переводим демо-время…' : '');

  return (
    <>
      <header className="topbar">
        <span className="workspace-icon" aria-hidden="true">A</span>
        <strong>Avito</strong>
        <span className="demo-label">Демо Slack · синтетические данные</span>
        <label className="profile-label" htmlFor="profile">
          Тестовый профиль
        </label>
        <select
          id="profile"
          value={state.profile.id}
          disabled={busy}
          onChange={(e) => void switchProfile(e.target.value)}
        >
          {state.profiles.map((p) => (
            <option key={p.id} value={p.id}>
              {profileLabel(p)}
            </option>
          ))}
        </select>
      </header>
      <div className="demo-clock" aria-label="Демо-время">
        <span>
          Демо-время: <strong>{state.clock.label}</strong>
          {state.clock.offset_days > 0 && ` (+${state.clock.offset_days} дн.)`}
        </span>
        <button type="button" disabled={busy} onClick={() => clock('next_day')}>
          Следующий день
        </button>
        {state.clock.offset_days > 0 && (
          <button type="button" disabled={busy} onClick={() => clock('reset')}>
            Сбросить время
          </button>
        )}
      </div>
      <div className="layout">
        <aside className="sidebar" aria-label="Рабочее пространство">
          <nav aria-label="Каналы и личные сообщения">
            <h2>Каналы</h2>
            {state.channels.map((c) => (
              <button
                key={c.id}
                type="button"
                aria-current={c.id === conversation}
                onClick={() => setConversation(c.id)}
              >
                # {c.name}
              </button>
            ))}
            <h2>Личные сообщения</h2>
            <button type="button" aria-current={inBot} onClick={() => setConversation(BOT)}>
              <span className="bot-icon" aria-hidden="true">✦</span> AI-buddy
              {unread > 0 && (
                <span className="count" aria-label="Новые отчёты">
                  {unread}
                </span>
              )}
            </button>
          </nav>
        </aside>
        <main className="conversation">
          <header className="conversation-header">
            <div>
              <h1>{channel ? `# ${channel.name}` : 'AI-buddy'}</h1>
              <p>{channel ? 'Канал команды «Поиск»' : 'Личные сообщения'}</p>
            </div>
            <div className="conversation-actions">
              {resetVisible && (
                <button type="button" disabled={busy} onClick={() => void reset('/api/chat/reset', 'Сбрасываем переписку…')}>
                  Сбросить чат
                </button>
              )}
              {inBot && !lead && (
                <button
                  type="button"
                  disabled={busy}
                  title="Очистить чат и снять тестовое назначение"
                  onClick={() => void reset('/api/pick/reset', 'Сбрасываем подбор…')}
                >
                  Начать подбор заново
                </button>
              )}
            </div>
          </header>
          <section className="messages" ref={messages} aria-label="Сообщения" aria-live="polite">
            {inBot && <AssignmentPanel state={state} busy={busy} onMove={move} onClose={closeBlocker} />}
            {inBot && <HelpRequests state={state} busy={busy} onClose={closeBlocker} />}
            {inBot && <TeamPanel state={state} />}
            <ol>
              <History state={state} conversation={conversation} />
            </ol>
          </section>
          <div className="compose-area">
            {notice && (
              <p className={notice.error ? 'notice error' : 'notice'} role="status">
                {notice.text}
              </p>
            )}
            {inBot && !lead && (
              <form onSubmit={send}>
                <label className="visually-hidden" htmlFor="message">
                  Сообщение AI-buddy
                </label>
                <div className="input-row">
                  <textarea
                    id="message"
                    ref={input}
                    rows={2}
                    maxLength={1200}
                    required
                    placeholder="Написать AI-buddy"
                    value={draft}
                    readOnly={busy}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' && !e.shiftKey) send(e);
                    }}
                  />
                  <button type="submit" className="send" disabled={busy}>
                    Отправить
                  </button>
                </div>
                <div className="compose-footer">
                  {!state.assignment && (
                    <button
                      type="button"
                      className="suggestion"
                      disabled={busy}
                      onClick={() => {
                        setDraft(STARTER_REQUEST);
                        input.current?.focus();
                      }}
                    >
                      {STARTER_REQUEST}
                    </button>
                  )}
                  <small className="mode">
                    {state.mode === 'fixture' ? 'Демо · тестовый подбор без модели' : 'Подбор через OpenRouter'}
                  </small>
                </div>
              </form>
            )}
          </div>
        </main>
      </div>
    </>
  );
}
