import type { AbsenceRequest } from '../api/contract';
import { useOnboardingState } from '../api/endpoints';
import { formatDate, formatTimeRange } from '../shared/format';
import { StatusBadge } from '../shared/StatusBadge';

export function RequestsPanel() {
  const state = useOnboardingState();
  const request = state.data?.scenarios.study_absence?.request ?? null;
  const nextStep = state.data?.next_step;

  return (
    <section className="requests" aria-labelledby="requests-title">
      <h2 id="requests-title">Мои заявки</h2>

      {state.isPending && (
        <p className="muted" role="status">
          Загружаем заявки…
        </p>
      )}

      {state.isError && (
        <div className="inline-error" role="alert">
          <p>Не удалось загрузить заявки.</p>
          <button type="button" className="button-link" onClick={() => void state.refetch()}>
            Повторить
          </button>
        </div>
      )}

      {state.isSuccess &&
        (request ? (
          <RequestSummary request={request} />
        ) : (
          <p className="muted">Заявок пока нет. Напишите в чат, что нужно оформить.</p>
        ))}

      {nextStep && (
        <div className="next-step">
          <h3>Следующий шаг</h3>
          <p>{nextStep}</p>
        </div>
      )}
    </section>
  );
}

function RequestSummary({ request }: { request: AbsenceRequest }) {
  const parts: string[] = [];
  if (request.date) parts.push(formatDate(request.date));
  if (request.start_time && request.end_time) parts.push(formatTimeRange(request.start_time, request.end_time));

  return (
    <div className="request">
      <div className="request-top">
        <p className="request-kind">Учебный отгул</p>
        <StatusBadge status={request.status} />
      </div>
      <p className="request-number">Заявка №{request.request_number}</p>
      {parts.length > 0 && <p className="request-when">{parts.join(', ')}</p>}
    </div>
  );
}
