import { describeStatus } from './status';

export function StatusBadge({ status }: { status: string }) {
  const { label, tone } = describeStatus(status);
  return <span className={`badge badge-${tone}`}>{label}</span>;
}
