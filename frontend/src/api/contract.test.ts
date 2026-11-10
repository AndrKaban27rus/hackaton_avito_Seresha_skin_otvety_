import { describe, expect, it } from 'vitest';
import { AbsenceRequestSchema, ChatResponseSchema, StateSchema, toCard } from './contract';

const draft = { draft_id: 'd1', date: '2030-11-20', start_time: '14:00', end_time: '18:00' };

describe('toCard', () => {
  it('распознаёт черновик отсутствия', () => {
    expect(toCard({ kind: 'absence_draft', data: draft })).toEqual({ kind: 'absence_draft', data: draft });
  });

  it('помечает неизвестный вид карточки', () => {
    expect(toCard({ kind: 'flag_draft', data: {} })).toEqual({ kind: 'unsupported', originalKind: 'flag_draft' });
  });

  it('помечает черновик с битыми данными', () => {
    expect(toCard({ kind: 'absence_draft', data: { draft_id: 'd1' } })).toEqual({
      kind: 'unsupported',
      originalKind: 'absence_draft',
    });
  });

  it('возвращает null без карточки', () => {
    expect(toCard(null)).toBeNull();
    expect(toCard(undefined)).toBeNull();
  });
});

describe('схемы ответов', () => {
  it('ответ чата без источников и карточки получает значения по умолчанию', () => {
    expect(ChatResponseSchema.parse({ reply: 'Привет' })).toEqual({ reply: 'Привет', sources: [], card: null });
  });

  it('номер заявки приводится к строке', () => {
    expect(AbsenceRequestSchema.parse({ request_number: 12, status: 'pending_approval' }).request_number).toBe('12');
  });

  it('минимальное состояние разбирается', () => {
    expect(StateSchema.parse({})).toEqual({ scenarios: {} });
  });
});
