import { delay, http, HttpResponse } from 'msw';
import { parseAbsenceRequest } from './parseAbsence';
import type { MockStore } from './store';

// Обработчики повторяют контракт из docs/api-absence-scenario.md.
// Используются в демо-режиме браузера (`npm run dev:mock`) и в тестах.

const realistic = import.meta.env.MODE !== 'test';
const pause = async (ms: number) => {
  if (realistic) await delay(ms);
};

const PROFILE = { display_name: 'Тестовый стажёр', grade: 'E1', team: 'Delivery, backend' };
const SOURCE = { source_id: 'demo-study-absence', title: 'Тестовая инструкция: учебный отгул' };

export const HELP_REPLY =
  'Этого я пока не умею. Для учебного отгула напишите дату и время, например: «Мне нужен учебный отгул 20 ноября 2030 года с 14:00 до 18:00».';

export function createHandlers(store: MockStore) {
  return [
    http.get('*/api/state', async () => {
      await pause(200);
      const request = store.latest();
      return HttpResponse.json({
        profile: PROFILE,
        scenarios: { study_absence: { request } },
        next_step: request
          ? 'Дождитесь согласования учебного отгула руководителем.'
          : 'Оформите учебный отгул через чат.',
      });
    }),

    http.post('*/api/chat', async ({ request }) => {
      await pause(700);
      const body = (await request.json().catch(() => null)) as { message?: unknown } | null;
      const message = typeof body?.message === 'string' ? body.message.trim() : '';
      if (!message) {
        return HttpResponse.json(
          { error: { code: 'validation_error', message: 'Сообщение не должно быть пустым.' } },
          { status: 400 },
        );
      }

      const parsed = parseAbsenceRequest(message, store.now());
      if (parsed === null) return HttpResponse.json({ reply: HELP_REPLY, sources: [], card: null });
      if (!parsed.ok) return HttpResponse.json({ reply: parsed.reason, sources: [SOURCE], card: null });

      const draft = store.createDraft(parsed.period);
      return HttpResponse.json({
        reply: 'Подготовил черновик. Проверьте дату и время и отправьте заявку.',
        sources: [SOURCE],
        card: { kind: 'absence_draft', data: draft },
      });
    }),

    http.post('*/api/onboarding-actions/confirm', async ({ request }) => {
      await pause(500);
      const body = (await request.json().catch(() => null)) as { draft_id?: unknown } | null;
      const draftId = typeof body?.draft_id === 'string' ? body.draft_id : '';
      const outcome = store.confirm(draftId);
      if (!outcome.ok) {
        return HttpResponse.json(
          { error: { code: outcome.code, message: 'Черновик больше не действует.' } },
          { status: outcome.code === 'draft_expired' ? 410 : 404 },
        );
      }
      return HttpResponse.json({ kind: 'absence_request', data: outcome.request });
    }),
  ];
}
