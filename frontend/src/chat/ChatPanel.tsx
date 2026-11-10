import { type FormEvent, useEffect, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { sendChatMessage } from '../api/endpoints';
import { describeError } from '../shared/errors';
import { MessageView } from './MessageView';
import { useChat } from './store';

export const EXAMPLE_REQUEST = 'Мне нужен учебный отгул 20 ноября 2030 года с 14:00 до 18:00';

export function ChatPanel() {
  const messages = useChat((state) => state.messages);
  const addUserMessage = useChat((state) => state.addUserMessage);
  const addAssistantReply = useChat((state) => state.addAssistantReply);
  const [input, setInput] = useState('');
  const [failedText, setFailedText] = useState<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const send = useMutation({
    mutationFn: sendChatMessage,
    onMutate: () => setFailedText(null),
    onSuccess: (reply) => addAssistantReply(reply),
    onError: (_error, text) => setFailedText(text),
  });

  useEffect(() => {
    bottomRef.current?.scrollIntoView?.({ block: 'end' });
  }, [messages.length, send.isPending, failedText]);

  function submit(text: string) {
    const message = text.trim();
    if (!message || send.isPending) return;
    addUserMessage(message);
    setInput('');
    send.mutate(message);
  }

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    submit(input);
  }

  const failure = failedText ? describeError(send.error) : null;

  return (
    <section className="chat" aria-labelledby="chat-title">
      <header className="chat-head">
        <h2 id="chat-title">Чат с buddy</h2>
        <p>Отвечает по тестовым инструкциям команды</p>
      </header>

      <div className="messages" role="log" aria-live="polite">
        {messages.map((message) => (
          <MessageView key={message.id} message={message} />
        ))}
        {send.isPending && (
          <p className="message assistant pending" role="status">
            Buddy отвечает…
          </p>
        )}
        {failure && failedText && (
          <div className="message assistant failed" role="alert">
            <p>{failure.message}</p>
            {failure.retryable && (
              <button type="button" className="button-link" onClick={() => send.mutate(failedText)}>
                Повторить
              </button>
            )}
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      <div className="composer-area">
        <div className="suggestions">
          <button
            type="button"
            className="chip"
            disabled={send.isPending}
            onClick={() => submit(EXAMPLE_REQUEST)}
          >
            {EXAMPLE_REQUEST}
          </button>
        </div>
        <form className="composer" onSubmit={onSubmit}>
          <input
            value={input}
            onChange={(event) => setInput(event.target.value)}
            maxLength={2000}
            placeholder="Напишите, что нужно оформить"
            aria-label="Сообщение для buddy"
          />
          <button type="submit" className="button" disabled={!input.trim() || send.isPending}>
            Отправить
          </button>
        </form>
      </div>
    </section>
  );
}
