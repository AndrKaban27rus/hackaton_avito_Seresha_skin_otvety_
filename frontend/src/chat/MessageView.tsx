import { CardView } from '../cards/CardView';
import type { ChatMessage } from './store';

export function MessageView({ message }: { message: ChatMessage }) {
  if (message.role === 'user') {
    return <p className="message user">{message.text}</p>;
  }

  return (
    <div className="message assistant">
      <p>{message.text}</p>
      {message.sources.length > 0 && (
        <ul className="sources" aria-label="Источники">
          {message.sources.map((source) => (
            <li key={source.source_id ?? source.title}>
              Источник: {source.title} <span className="tag">тестовый материал</span>
            </li>
          ))}
        </ul>
      )}
      {message.card && <CardView card={message.card} />}
    </div>
  );
}
