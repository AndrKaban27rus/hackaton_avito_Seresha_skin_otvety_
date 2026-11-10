import { QueryClientProvider } from '@tanstack/react-query';
import { render } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { App } from '../app/App';
import { createQueryClient } from '../app/queryClient';

export function renderApp() {
  const client = createQueryClient({ retry: false });
  const user = userEvent.setup();
  const view = render(
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>,
  );
  return { user, client, ...view };
}

export type TestUser = ReturnType<typeof userEvent.setup>;
