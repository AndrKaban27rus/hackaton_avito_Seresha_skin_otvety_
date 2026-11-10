import { ApiError } from '../api/client';

export interface ErrorView {
  message: string;
  /** Можно ли повторить тот же запрос. Подтверждение по draft_id идемпотентно, повтор безопасен. */
  retryable: boolean;
}

const EXPIRED_DRAFT = 'Черновик больше не действует. Попросите buddy подготовить заявку заново.';

export function describeError(error: unknown): ErrorView {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'draft_expired':
      case 'draft_not_found':
        return { message: EXPIRED_DRAFT, retryable: false };
      case 'forbidden':
        return { message: 'Это действие вам недоступно.', retryable: false };
      case 'conflict':
      case 'validation_error':
        return { message: error.message, retryable: false };
      case 'service_unavailable':
        return { message: 'Сервис заявок сейчас недоступен. Попробуйте ещё раз.', retryable: true };
      case 'timeout':
      case 'llm_timeout':
        return { message: 'Сервер долго не отвечает. Попробуйте ещё раз.', retryable: true };
      case 'network':
        return { message: 'Нет связи с сервером. Проверьте подключение и попробуйте ещё раз.', retryable: true };
    }
  }
  return { message: 'Что-то пошло не так. Попробуйте ещё раз.', retryable: true };
}
