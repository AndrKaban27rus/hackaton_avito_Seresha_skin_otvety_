import { useQuery } from '@tanstack/react-query';
import { apiRequest } from './client';
import { ChatResponseSchema, ConfirmResponseSchema, StateSchema } from './contract';

// Ответ LLM может идти долго, поэтому у чата таймаут больше, чем у остальных запросов.
const CHAT_TIMEOUT_MS = 35_000;
const CONFIRM_TIMEOUT_MS = 15_000;

export const sendChatMessage = (message: string) =>
  apiRequest('/api/chat', ChatResponseSchema, {
    method: 'POST',
    body: { message },
    timeoutMs: CHAT_TIMEOUT_MS,
  });

export const confirmDraft = (draftId: string) =>
  apiRequest('/api/onboarding-actions/confirm', ConfirmResponseSchema, {
    method: 'POST',
    body: { draft_id: draftId },
    timeoutMs: CONFIRM_TIMEOUT_MS,
  });

export const fetchState = () => apiRequest('/api/state', StateSchema);

export const stateQueryKey = ['state'] as const;

export const useOnboardingState = () => useQuery({ queryKey: stateQueryKey, queryFn: fetchState });
