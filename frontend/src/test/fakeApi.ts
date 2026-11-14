import { vi } from 'vitest';
import type { Assignment, State } from '../api';

const profiles: State['profiles'] = [
  { id: 'intern_01', name: 'Алексей Сергеевич Ким', role: 'intern', grade: 'E1' },
  { id: 'lead_01', name: 'Мария Андреевна Волкова', role: 'lead', grade: '' },
  { id: 'mentor_01', name: 'Олег Павлович Смирнов', role: 'mentor', grade: 'E3' },
  { id: 'intern_02', name: 'Елена Игоревна Орлова', role: 'intern', grade: 'E1' },
];

const channels: State['channels'] = [
  { id: 'general', name: 'общий', messages: [{ author: 'Мария Волкова', text: 'Привет, команда!' }] },
];

const NEXT: Record<string, { status: string; name: string; action?: string; step: string }> = {
  assigned: { status: 'in_progress', name: 'Назначена', action: 'Взять в работу', step: 'замерь p95.' },
  in_progress: { status: 'review', name: 'В работе', action: 'Отправить на ревью', step: 'приложи результат.' },
  review: { status: '', name: 'На ревью', step: 'дождись ревьюера.' },
};

const DAYS = ['16 ноября, пн', '17 ноября, вт', '18 ноября, ср', '19 ноября, чт', '20 ноября, пт', '21 ноября, сб'];

export type Call = { path: string; body?: Record<string, string> };
type Reply = { status: number; body: unknown } | 'network';

/** A tiny in-memory AI-buddy that answers like the Go API in fixture mode. */
export function fakeApi(options: { session?: string } = {}) {
  let session = options.session ?? null;
  const chats: Record<string, State['chat']> = {};
  const events: Record<string, State['events']> = {};
  const assignments: Record<string, { ticket_id?: string; status: string } | null> = {};
  let reports: State['notifications'] = [];
  let blockers: NonNullable<State['blocker']>[] = [];
  let offset = 0;
  let tick = 0;
  const calls: Call[] = [];
  const overrides: Reply[] = [];
  const at = () => `2026-11-16T10:00:${String(tick++).padStart(2, '0')}Z`;

  const view = (raw: { ticket_id?: string; status: string } | null | undefined): Assignment | null => {
    if (!raw) return null;
    if (!raw.ticket_id) return { status: 'restricted', status_name: 'Недоступна', days_left: 0, overdue: false };
    const step = NEXT[raw.status];
    const daysLeft = 3 - offset;
    return {
      ticket_id: raw.ticket_id,
      title: 'Проверить кэширование',
      status: raw.status,
      status_name: step.name,
      due_date: '2026-11-19',
      due_label: DAYS[3],
      days_left: daysLeft,
      overdue: daysLeft < 0 && raw.status !== 'review',
      next_step: step.step,
      next_status: step.status || undefined,
      action: step.action,
      resources: [
        { id: 'grafana-search-filters', kind: 'grafana', title: 'Тестовый дашборд Grafana', url: '/demo/grafana-search-filters' },
      ],
    };
  };

  const state = (): State => {
    const profile = profiles.find((p) => p.id === session)!;
    const lead = profile.role === 'lead';
    return {
      profile,
      profiles,
      channels,
      chat: chats[profile.id] ?? [],
      notifications: lead ? reports : [],
      assignment: lead ? null : view(assignments[profile.id]),
      events: lead ? [] : (events[profile.id] ?? []),
      team: lead
        ? profiles
            .filter((p) => p.role === 'intern')
            .map((p) => {
              const a = view(assignments[p.id]);
              return {
                intern_id: p.id,
                name: p.name,
                ticket_id: a?.ticket_id,
                title: a?.title,
                status: a?.status ?? 'none',
                status_name: a?.status_name ?? 'Без задачи',
                due_label: a?.due_label,
                overdue: a?.overdue ?? false,
              };
            })
        : [],
      blocker: blockers.find((b) => b.intern_name === profile.id && ['clarifying', 'open'].includes(b.status)) ?? null,
      help_requests: profile.role === 'mentor' ? blockers.filter((b) => b.status === 'open') : [],
      clock: { today: '2026-11-16', label: DAYS[Math.min(offset, DAYS.length - 1)], offset_days: offset },
      mode: 'fixture',
    };
  };

  const handle = (path: string, body?: Record<string, string>): Reply => {
    if (path === '/api/session') {
      session = body!.profile;
      return { status: 200, body: state() };
    }
    if (!session) return { status: 401, body: { error: 'Выберите тестовый профиль.' } };
    const user = session;
    if (path === '/api/state') return { status: 200, body: state() };
    if (path === '/api/chat') {
      const chat = (chats[user] ??= []);
      let answer = assignments[user] ? 'Открой тестовый набор запросов: /demo/conf-search-p95.' : 'Могу подобрать стартовую задачу.';
      const active = blockers.find((b) => b.intern_name === user && b.status === 'clarifying');
      if (active) {
        active.status = 'open';
        active.message = `Нужна помощь стажёру\nПрепятствие: ${active.obstacle}. ${body!.message}`;
        answer = 'Записал блокер и написал наставнику.';
      } else if (body!.message.startsWith('Не могу')) {
        blockers.push({ id: blockers.length + 1, ticket_id: 'BACK-112', status: 'clarifying', obstacle: body!.message,
          helper_name: 'Олег Павлович Смирнов', intern_name: user, created_at: at() });
        answer = 'Уточни, что именно мешает.';
      }
      if (body!.message.includes('задач') && !assignments[user]) {
        assignments[user] = { ticket_id: 'BACK-112', status: 'assigned' };
        answer = 'Назначена BACK-112 — «Проверить кэширование».\nПервый шаг: замерь p95.';
        reports = [...reports, { message: 'Отчёт: назначена BACK-112', created_at: at() }];
      }
      chat.push({ question: body!.message, answer, mode: 'fixture', created_at: at() });
      return { status: 200, body: state() };
    }
    if (path === '/api/task/status') {
      const a = assignments[user];
      if (!a || a.ticket_id !== body!.ticket_id) return { status: 403, body: { error: 'Не твоя задача.' } };
      a.status = body!.status;
      (events[user] ??= []).push({ kind: 'status', text: `Jira: BACK-112 переведена в «${NEXT[a.status].name}».`, created_at: at() });
      return { status: 200, body: state() };
    }
    if (path === '/api/blocker/close') {
      const b = blockers.find((x) => x.id === Number(body!.blocker_id));
      if (!b) return { status: 403, body: { error: 'Чужой блокер.' } };
      b.status = 'closed';
      (events[b.intern_name!] ??= []).push({ kind: 'blocker', text: 'Блокер по BACK-112 снят.', created_at: at() });
      return { status: 200, body: state() };
    }
    if (path === '/api/clock') {
      offset = body!.action === 'reset' ? 0 : offset + 1;
      const a = assignments['intern_01'];
      if (offset === 2 && a) {
        (events['intern_01'] ??= []).push({ kind: 'due_soon', text: 'Напоминание: завтра срок по BACK-112.', created_at: at() });
      }
      return { status: 200, body: state() };
    }
    if (path === '/api/chat/reset') {
      if (profiles.find((p) => p.id === user)!.role === 'lead') reports = [];
      else {
        chats[user] = [];
        events[user] = [];
      }
      return { status: 200, body: state() };
    }
    if (path === '/api/pick/reset') {
      chats[user] = [];
      events[user] = [];
      assignments[user] = null;
      return { status: 200, body: state() };
    }
    return { status: 404, body: { error: 'Действие не найдено.' } };
  };

  const fetchMock = vi.fn(async (path: string, init?: RequestInit) => {
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, string>) : undefined;
    calls.push({ path, body });
    const reply = overrides.shift() ?? handle(path, body);
    if (reply === 'network') throw new TypeError('Failed to fetch');
    return new Response(typeof reply.body === 'string' ? reply.body : JSON.stringify(reply.body), {
      status: reply.status,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  vi.stubGlobal('fetch', fetchMock);

  return {
    calls,
    /** Makes the next request fail without touching the fake state. */
    failNext(reply: Reply) {
      overrides.push(reply);
    },
    setAssignment(user: string, value: { ticket_id?: string; status: string } | null) {
      assignments[user] = value;
    },
  };
}
