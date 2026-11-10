import { setupServer } from 'msw/node';
import { createHandlers } from '../mocks/handlers';
import { createMockStore } from '../mocks/store';

export const mockStore = createMockStore();
export const server = setupServer(...createHandlers(mockStore));
