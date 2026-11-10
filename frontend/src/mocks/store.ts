import type { AbsencePeriod } from './parseAbsence';

// Имитация хранилища бэкенда для демо-режима и тестов.

export interface MockDraft extends AbsencePeriod {
  draft_id: string;
  expires_at: string;
}

export interface MockRequest extends AbsencePeriod {
  request_number: number;
  status: 'pending_approval';
}

interface MockData {
  lastNumber: number;
  draftSeq: number;
  drafts: Record<string, MockDraft>;
  confirmed: Record<string, MockRequest>;
  latest: MockRequest | null;
}

export type ConfirmOutcome =
  | { ok: true; request: MockRequest }
  | { ok: false; code: 'draft_not_found' | 'draft_expired' };

export interface MockStore {
  now: () => Date;
  createDraft: (period: AbsencePeriod) => MockDraft;
  confirm: (draftId: string) => ConfirmOutcome;
  latest: () => MockRequest | null;
  reset: () => void;
}

export const STORAGE_KEY = 'ai-buddy.mock-db';
const DRAFT_TTL_MS = 10 * 60 * 1000;

// Первая заявка получает №12, как в примере команды.
const initialData = (): MockData => ({ lastNumber: 11, draftSeq: 0, drafts: {}, confirmed: {}, latest: null });

export function createMockStore(options: { persist?: boolean; now?: () => Date } = {}): MockStore {
  const now = options.now ?? (() => new Date());
  const persist = options.persist ?? false;
  let data = persist ? load() : initialData();
  const save = () => {
    if (persist) write(data);
  };

  return {
    now,
    createDraft(period) {
      data.draftSeq += 1;
      const draft: MockDraft = {
        ...period,
        draft_id: `drf-${data.draftSeq}-${Math.random().toString(36).slice(2, 8)}`,
        expires_at: new Date(now().getTime() + DRAFT_TTL_MS).toISOString(),
      };
      data.drafts[draft.draft_id] = draft;
      save();
      return draft;
    },
    confirm(draftId) {
      // Повторное подтверждение возвращает ту же заявку, как требует контракт.
      const existing = data.confirmed[draftId];
      if (existing) return { ok: true, request: existing };

      const draft = data.drafts[draftId];
      if (!draft) return { ok: false, code: 'draft_not_found' };
      if (Date.parse(draft.expires_at) <= now().getTime()) return { ok: false, code: 'draft_expired' };

      data.lastNumber += 1;
      const request: MockRequest = {
        request_number: data.lastNumber,
        status: 'pending_approval',
        date: draft.date,
        start_time: draft.start_time,
        end_time: draft.end_time,
      };
      data.confirmed[draftId] = request;
      data.latest = request;
      save();
      return { ok: true, request };
    },
    latest: () => data.latest,
    reset() {
      data = initialData();
      if (persist) remove();
    },
  };
}

function load(): MockData {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Partial<MockData>;
      if (typeof parsed.lastNumber === 'number') return { ...initialData(), ...parsed };
    }
  } catch {
    // Хранилище недоступно или данные испорчены: начинаем с чистого состояния.
  }
  return initialData();
}

function write(data: MockData) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(data));
  } catch {
    // Без хранилища демо работает, но не переживёт перезагрузку.
  }
}

function remove() {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Нечего очищать.
  }
}
