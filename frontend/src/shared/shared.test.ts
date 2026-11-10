import { describe, expect, it } from 'vitest';
import { ApiError } from '../api/client';
import { describeError } from './errors';
import { formatDate, formatTimeRange } from './format';
import { describeStatus } from './status';

describe('formatDate', () => {
  it('пишет дату словами', () => {
    expect(formatDate('2030-11-20')).toBe('20 ноября 2030');
    expect(formatDate('2031-01-05')).toBe('5 января 2031');
  });

  it('оставляет как есть то, что не похоже на дату', () => {
    expect(formatDate('завтра')).toBe('завтра');
    expect(formatDate('2030-13-01')).toBe('2030-13-01');
  });
});

describe('formatTimeRange', () => {
  it('соединяет время через тире', () => {
    expect(formatTimeRange('14:00', '18:00')).toBe('14:00–18:00');
  });
});

describe('describeStatus', () => {
  it('знает статусы заявки', () => {
    expect(describeStatus('pending_approval')).toEqual({ label: 'Ожидает согласования', tone: 'pending' });
    expect(describeStatus('approved')).toEqual({ label: 'Согласована', tone: 'success' });
    expect(describeStatus('rejected')).toEqual({ label: 'Отклонена', tone: 'danger' });
  });

  it('показывает неизвестный код как есть', () => {
    expect(describeStatus('archived')).toEqual({ label: 'archived', tone: 'neutral' });
  });
});

describe('describeError', () => {
  const view = (code: string, message = 'Текст сервера') => describeError(new ApiError(code, message));

  it('устаревший или чужой черновик нельзя повторить', () => {
    expect(view('draft_expired').retryable).toBe(false);
    expect(view('draft_not_found').message).toContain('Черновик больше не действует');
  });

  it('отказ в правах нельзя повторить', () => {
    expect(view('forbidden')).toEqual({ message: 'Это действие вам недоступно.', retryable: false });
  });

  it('конфликт и ошибка проверки показывают текст сервера', () => {
    expect(view('conflict', 'Пересекается с заявкой №10')).toEqual({
      message: 'Пересекается с заявкой №10',
      retryable: false,
    });
    expect(view('validation_error', 'Дата уже прошла').message).toBe('Дата уже прошла');
  });

  it('временные сбои можно повторить', () => {
    for (const code of ['service_unavailable', 'timeout', 'llm_timeout', 'network', 'http_500']) {
      expect(view(code).retryable).toBe(true);
    }
  });

  it('неизвестная ошибка получает общий текст', () => {
    expect(describeError(new Error('x'))).toEqual({
      message: 'Что-то пошло не так. Попробуйте ещё раз.',
      retryable: true,
    });
  });
});
