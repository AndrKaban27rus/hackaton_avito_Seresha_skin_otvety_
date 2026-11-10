import { afterEach, describe, expect, it } from 'vitest';
import { parseAbsenceRequest } from './parseAbsence';
import { createMockStore, STORAGE_KEY } from './store';

const today = new Date(2026, 10, 11);
const period = { date: '2030-11-20', start_time: '14:00', end_time: '18:00' };

describe('parseAbsenceRequest', () => {
  it('разбирает пример команды', () => {
    expect(parseAbsenceRequest('Мне нужен учебный отгул 20 ноября 2030 года с 14:00 до 18:00', today)).toEqual({
      ok: true,
      period,
    });
  });

  it('понимает дату в ISO и время через точку', () => {
    expect(parseAbsenceRequest('отгул 2030-12-01 с 9.30 до 11:00', today)).toEqual({
      ok: true,
      period: { date: '2030-12-01', start_time: '09:30', end_time: '11:00' },
    });
  });

  it('без года берёт текущий', () => {
    const result = parseAbsenceRequest('учебное отсутствие 25 декабря с 10:00 до 12:00', today);
    expect(result).toEqual({ ok: true, period: { date: '2026-12-25', start_time: '10:00', end_time: '12:00' } });
  });

  it('не реагирует на сообщения не про отсутствие', () => {
    expect(parseAbsenceRequest('Как дела?', today)).toBeNull();
  });

  it.each([
    ['учебный отгул с 14:00 до 18:00', 'Укажите дату'],
    ['учебный отгул 20 ноября 2030', 'Укажите время'],
    ['учебный отгул 31 ноября 2030 с 14:00 до 18:00', 'Такой даты нет'],
    ['учебный отгул 20 ноября 2030 с 25:00 до 26:00', 'Время указано с ошибкой'],
    ['учебный отгул 20 ноября 2030 с 18:00 до 14:00', 'позже времени начала'],
    ['учебный отгул 1 января 2020 с 10:00 до 12:00', 'уже прошла'],
  ])('объясняет, что не так: «%s»', (text, expected) => {
    const result = parseAbsenceRequest(text, today);
    expect(result).not.toBeNull();
    expect(result && !result.ok && result.reason).toContain(expected);
  });
});

describe('createMockStore', () => {
  afterEach(() => localStorage.clear());

  it('выдаёт номера по порядку и повторно возвращает ту же заявку', () => {
    const store = createMockStore();
    const first = store.createDraft(period);
    const second = store.createDraft(period);

    const confirmed = store.confirm(first.draft_id);
    expect(confirmed).toMatchObject({ ok: true, request: { request_number: 12, status: 'pending_approval' } });
    expect(store.confirm(first.draft_id)).toEqual(confirmed);
    expect(store.confirm(second.draft_id)).toMatchObject({ ok: true, request: { request_number: 13 } });
    expect(store.latest()?.request_number).toBe(13);
  });

  it('не знает чужих черновиков', () => {
    expect(createMockStore().confirm('missing')).toEqual({ ok: false, code: 'draft_not_found' });
  });

  it('черновик истекает через 10 минут', () => {
    let now = new Date('2030-01-01T10:00:00Z');
    const store = createMockStore({ now: () => now });
    const draft = store.createDraft(period);
    now = new Date('2030-01-01T10:11:00Z');
    expect(store.confirm(draft.draft_id)).toEqual({ ok: false, code: 'draft_expired' });
  });

  it('в демо-режиме сохраняет данные между перезагрузками', () => {
    const first = createMockStore({ persist: true });
    first.confirm(first.createDraft(period).draft_id);

    const reloaded = createMockStore({ persist: true });
    expect(reloaded.latest()?.request_number).toBe(12);

    reloaded.reset();
    expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
    expect(createMockStore({ persist: true }).latest()).toBeNull();
  });

  it('при испорченных данных начинает с чистого состояния', () => {
    localStorage.setItem(STORAGE_KEY, '{broken');
    expect(createMockStore({ persist: true }).latest()).toBeNull();
  });
});
