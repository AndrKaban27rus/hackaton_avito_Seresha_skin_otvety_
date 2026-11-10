import React from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import { createQueryClient } from './app/queryClient';
import { router } from './router';
import './style.css';

/** `npm run dev:mock`: API подменяется моками в браузере, бэкенд не нужен. */
async function enableMocking() {
  if (import.meta.env.MODE !== 'mock') return;
  const { worker } = await import('./mocks/browser');
  await worker.start({ onUnhandledRequest: 'bypass' });
}

function render() {
  createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <QueryClientProvider client={createQueryClient()}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </React.StrictMode>,
  );
}

// Если моки не запустились (например, нет public/mockServiceWorker.js), интерфейс всё равно открывается.
void enableMocking()
  .catch((error: unknown) => console.error('Не удалось включить моки API', error))
  .then(render);
