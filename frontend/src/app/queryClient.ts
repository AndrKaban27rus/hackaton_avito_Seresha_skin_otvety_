import { QueryClient } from '@tanstack/react-query';

export function createQueryClient({ retry = 1 }: { retry?: number | boolean } = {}) {
  return new QueryClient({
    defaultOptions: {
      queries: { retry, refetchOnWindowFocus: false },
      // Действия не повторяем автоматически: повтор только по кнопке, чтобы стажёр видел, что происходит.
      mutations: { retry: false },
    },
  });
}
