import { create } from 'zustand';
import type { AbsenceRequest, Card, ChatResponse, Source } from '../api/contract';

export type ChatMessage =
  | { id: string; role: 'user'; text: string }
  | { id: string; role: 'assistant'; text: string; sources: Source[]; card: Card | null };

const WELCOME: ChatMessage = {
  id: 'welcome',
  role: 'assistant',
  text: 'Привет! Я помогу с делами первых недель. Пока умею оформлять учебный отгул: напишите дату и время.',
  sources: [],
  card: null,
};

interface ChatState {
  messages: ChatMessage[];
  /** Результаты подтверждения по draft_id: карточка помнит номер заявки, пока открыта страница. */
  results: Record<string, AbsenceRequest>;
  addUserMessage: (text: string) => void;
  addAssistantReply: (reply: ChatResponse) => void;
  saveResult: (draftId: string, result: AbsenceRequest) => void;
  reset: () => void;
}

// crypto.randomUUID есть не везде (например, по http на IP-адресе), поэтому простой счётчик.
let sequence = 0;
const nextId = () => `msg-${++sequence}`;

export const useChat = create<ChatState>()((set) => ({
  messages: [WELCOME],
  results: {},
  addUserMessage: (text) =>
    set((state) => ({ messages: [...state.messages, { id: nextId(), role: 'user', text }] })),
  addAssistantReply: (reply) =>
    set((state) => ({
      messages: [
        ...state.messages,
        { id: nextId(), role: 'assistant', text: reply.reply, sources: reply.sources, card: reply.card },
      ],
    })),
  saveResult: (draftId, result) =>
    set((state) => ({ results: { ...state.results, [draftId]: result } })),
  reset: () => set({ messages: [WELCOME], results: {} }),
}));
