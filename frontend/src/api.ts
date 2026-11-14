import { z } from 'zod';

const ProfileSchema = z.object({
  id: z.string(),
  name: z.string(),
  role: z.enum(['intern', 'lead', 'mentor']),
  team: z.string().optional(),
  grade: z.string(),
});

const AssignmentSchema = z.object({
  ticket_id: z.string().optional(),
  title: z.string().optional(),
  status: z.string(),
  status_name: z.string(),
  first_step: z.string().optional(),
  due_date: z.string().optional(),
  due_label: z.string().optional(),
  days_left: z.number(),
  overdue: z.boolean(),
  next_step: z.string().optional(),
  next_status: z.string().optional(),
  action: z.string().optional(),
  resources: z.array(z.object({ id: z.string(), kind: z.string(), title: z.string(), url: z.string() })).optional(),
});

const BlockerSchema = z.object({
  id: z.number(),
  ticket_id: z.string(),
  status: z.enum(['clarifying', 'open', 'self_help', 'closed']),
  obstacle: z.string(),
  helper_name: z.string().optional(),
  intern_name: z.string().optional(),
  message: z.string().optional(),
  created_at: z.string(),
});

const TeamRowSchema = z.object({
  intern_id: z.string(),
  name: z.string(),
  ticket_id: z.string().optional(),
  title: z.string().optional(),
  status: z.string(),
  status_name: z.string(),
  due_label: z.string().optional(),
  overdue: z.boolean(),
});

export const StateSchema = z.object({
  profile: ProfileSchema,
  profiles: z.array(ProfileSchema),
  channels: z.array(
    z.object({
      id: z.string(),
      name: z.string(),
      messages: z.array(z.object({ author: z.string(), text: z.string() })),
    }),
  ),
  chat: z.array(z.object({ question: z.string(), answer: z.string(), mode: z.string(), created_at: z.string() })),
  notifications: z.array(z.object({ message: z.string(), created_at: z.string() })),
  assignment: AssignmentSchema.nullable(),
  events: z.array(
    z.object({ kind: z.enum(['status', 'due_soon', 'overdue', 'blocker']), text: z.string(), created_at: z.string() }),
  ),
  blocker: BlockerSchema.nullable(),
  help_requests: z.array(BlockerSchema),
  team: z.array(TeamRowSchema),
  clock: z.object({ today: z.string(), label: z.string(), offset_days: z.number() }),
  mode: z.enum(['fixture', 'openrouter']),
});

export type State = z.infer<typeof StateSchema>;
export type Profile = z.infer<typeof ProfileSchema>;
export type Assignment = z.infer<typeof AssignmentSchema>;
export type TeamRow = z.infer<typeof TeamRowSchema>;
export type Blocker = z.infer<typeof BlockerSchema>;

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export const NETWORK_ERROR = 'Связь с сервером прервалась. Повторите запрос.';

/** Calls the AI-buddy API and validates the returned state. */
export async function api(path: string, payload?: object): Promise<State> {
  const init: RequestInit =
    payload === undefined
      ? {}
      : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) };
  let response: Response;
  try {
    response = await fetch(path, init);
  } catch {
    throw new ApiError(NETWORK_ERROR, 0);
  }
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const message =
      body && typeof body === 'object' && 'error' in body && typeof body.error === 'string'
        ? body.error
        : 'Запрос не выполнен. Попробуйте снова.';
    throw new ApiError(message, response.status);
  }
  const parsed = StateSchema.safeParse(body);
  if (!parsed.success) throw new ApiError('Сервер вернул неожиданный ответ. Обновите страницу.', response.status);
  return parsed.data;
}

export const DEFAULT_PROFILE = 'intern_02';

/** Opens the saved session or starts the default demo profile. */
export async function loadState(): Promise<State> {
  try {
    return await api('/api/state');
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return api('/api/session', { profile: DEFAULT_PROFILE });
    throw error;
  }
}

/** Human wording for the time left until the deadline. */
export function dueHint(a: Pick<Assignment, 'days_left' | 'overdue' | 'status'>): string {
  if (a.status === 'review') return 'задача на ревью';
  if (a.overdue) return `просрочено на ${-a.days_left} дн.`;
  if (a.days_left === 0) return 'срок сегодня';
  if (a.days_left === 1) return 'срок завтра';
  return `осталось ${a.days_left} дн.`;
}

export const EVENT_LABELS: Record<State['events'][number]['kind'], string> = {
  status: 'Jira',
  due_soon: 'Напоминание',
  overdue: 'Просрочка',
  blocker: 'Блокер',
};

const ROLE_LABELS: Record<Profile['role'], string> = { intern: 'Стажёр', lead: 'Руководитель', mentor: 'Наставник' };

/** Splits text into plain parts and links to synthetic demo pages. */
export function linkParts(text: string): { text: string; href?: string }[] {
  return text.split(/(\/demo\/[a-z0-9-]+)/).filter(Boolean).map((part) =>
    /^\/demo\/[a-z0-9-]+$/.test(part) ? { text: part, href: part } : { text: part },
  );
}

export function profileLabel(p: Profile): string {
  return `${p.name} · ${ROLE_LABELS[p.role]}${p.role === 'intern' ? ` ${p.grade}` : ''}`;
}

export function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });
}
