import { delay, http, HttpResponse } from 'msw';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';
import { server } from '../test/server';
import { apiRequest } from './client';
import { confirmDraft, sendChatMessage } from './endpoints';

const Ping = z.object({ ok: z.boolean() });

afterEach(() => {
  vi.unstubAllEnvs();
});

describe('apiRequest', () => {
  it('возвращает разобранный ответ и отправляет JSON', async () => {
    let received: unknown;
    server.use(
      http.post('*/api/ping', async ({ request }) => {
        received = await request.json();
        return HttpResponse.json({ ok: true });
      }),
    );

    await expect(apiRequest('/api/ping', Ping, { method: 'POST', body: { a: 1 } })).resolves.toEqual({ ok: true });
    expect(received).toEqual({ a: 1 });
  });

  it('берёт код и текст из структурированной ошибки', async () => {
    server.use(
      http.get('*/api/ping', () =>
        HttpResponse.json({ error: { code: 'conflict', message: 'Пересекается' } }, { status: 409 }),
      ),
    );
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({
      code: 'conflict',
      message: 'Пересекается',
      status: 409,
    });
  });

  it('понимает ошибку в виде строки', async () => {
    server.use(http.get('*/api/ping', () => HttpResponse.json({ error: 'down' }, { status: 502 })));
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({ code: 'http_502', message: 'down' });
  });

  it('подставляет код, если в ошибке нет текста', async () => {
    server.use(
      http.get('*/api/ping', () => HttpResponse.json({ error: { code: 'forbidden' } }, { status: 403 })),
    );
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({
      code: 'forbidden',
      message: 'Ошибка сервера (403)',
    });
  });

  it('обрабатывает ответ не в JSON', async () => {
    server.use(http.get('*/api/ping', () => new HttpResponse('oops', { status: 500 })));
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({
      code: 'http_500',
      message: 'Ошибка сервера (500)',
    });
  });

  it('отклоняет ответ в неожиданном формате', async () => {
    server.use(http.get('*/api/ping', () => HttpResponse.json({ nope: 1 })));
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({ code: 'bad_response' });
  });

  it('сообщает о потере связи', async () => {
    server.use(http.get('*/api/ping', () => HttpResponse.error()));
    await expect(apiRequest('/api/ping', Ping)).rejects.toMatchObject({ code: 'network' });
  });

  it('прерывает ожидание по таймауту', async () => {
    server.use(
      http.get('*/api/ping', async () => {
        await delay(200);
        return HttpResponse.json({ ok: true });
      }),
    );
    await expect(apiRequest('/api/ping', Ping, { timeoutMs: 20 })).rejects.toMatchObject({ code: 'timeout' });
  });

  it('ходит на адрес из VITE_API_URL, если он задан', async () => {
    vi.stubEnv('VITE_API_URL', 'http://api.test/');
    let url = '';
    server.use(
      http.get('*/api/ping', ({ request }) => {
        url = request.url;
        return HttpResponse.json({ ok: true });
      }),
    );
    await apiRequest('/api/ping', Ping);
    expect(url).toBe('http://api.test/api/ping');
  });
});

describe('Запросы к моку бэкенда', () => {
  it('пустое сообщение отклоняется проверкой', async () => {
    await expect(sendChatMessage('   ')).rejects.toMatchObject({ code: 'validation_error' });
  });

  it('неизвестный черновик не подтверждается', async () => {
    await expect(confirmDraft('missing')).rejects.toMatchObject({ code: 'draft_not_found', status: 404 });
  });
});
