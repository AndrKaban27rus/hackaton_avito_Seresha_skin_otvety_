import type { Card } from '../api/contract';
import { AbsenceDraftCard } from './AbsenceDraftCard';

export function CardView({ card }: { card: Card }) {
  if (card.kind === 'absence_draft') return <AbsenceDraftCard draft={card.data} />;
  return (
    <p className="slip-unsupported">
      Карточку типа «{card.originalKind}» интерфейс пока не показывает.
    </p>
  );
}
