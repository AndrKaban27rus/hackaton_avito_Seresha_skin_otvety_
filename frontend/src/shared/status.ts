export type StatusTone = 'pending' | 'success' | 'danger' | 'neutral';

const STATUSES: Record<string, { label: string; tone: StatusTone }> = {
  pending_approval: { label: 'Ожидает согласования', tone: 'pending' },
  approved: { label: 'Согласована', tone: 'success' },
  rejected: { label: 'Отклонена', tone: 'danger' },
};

/** Единый словарь статусов: код с бэкенда → подпись в интерфейсе. Неизвестный код показываем как есть. */
export function describeStatus(status: string): { label: string; tone: StatusTone } {
  return STATUSES[status] ?? { label: status, tone: 'neutral' };
}
