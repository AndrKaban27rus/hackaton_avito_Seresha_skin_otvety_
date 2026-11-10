import type { z } from 'zod';
import { ErrorBodySchema } from './contract';

export class ApiError extends Error {
  readonly code: string;
  readonly status?: number;

  constructor(code: string, message: string, status?: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST';
  body?: unknown;
  timeoutMs?: number;
}

/** По умолчанию API на том же адресе, что и интерфейс: в Docker /api проксирует nginx. */
function baseUrl(): string {
  const configured: string | undefined = import.meta.env.VITE_API_URL;
  if (configured) return configured.replace(/\/+$/, '');
  return window.location.origin;
}

export async function apiRequest<S extends z.ZodTypeAny>(
  path: string,
  schema: S,
  options: RequestOptions = {},
): Promise<z.output<S>> {
  const { method = 'GET', body, timeoutMs = 10_000 } = options;

  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new ApiError('timeout', 'Сервер не ответил вовремя')), timeoutMs);
  });

  let response: Response;
  try {
    response = await Promise.race([
      fetch(`${baseUrl()}${path}`, {
        method,
        headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
      }),
      timeout,
    ]);
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw new ApiError('network', 'Нет связи с сервером');
  } finally {
    clearTimeout(timer);
  }

  const payload: unknown = await response.json().catch(() => undefined);
  if (!response.ok) throw errorFromResponse(response.status, payload);

  const parsed = schema.safeParse(payload);
  if (!parsed.success) {
    throw new ApiError('bad_response', 'Сервер вернул ответ в неожиданном формате', response.status);
  }
  return parsed.data;
}

function errorFromResponse(status: number, payload: unknown): ApiError {
  const fallback = `Ошибка сервера (${status})`;
  const parsed = ErrorBodySchema.safeParse(payload);
  if (!parsed.success) return new ApiError(`http_${status}`, fallback, status);

  const { error } = parsed.data;
  if (typeof error === 'string') return new ApiError(`http_${status}`, error, status);
  return new ApiError(error.code, error.message ?? fallback, status);
}
