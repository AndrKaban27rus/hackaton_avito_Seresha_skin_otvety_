import { setupWorker } from 'msw/browser';
import { createHandlers } from './handlers';
import { createMockStore } from './store';

// Демо-режим: данные хранятся в localStorage, поэтому заявка видна и после перезагрузки.
export const worker = setupWorker(...createHandlers(createMockStore({ persist: true })));
