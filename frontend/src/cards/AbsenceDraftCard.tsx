import { useRef } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { AbsenceDraft } from '../api/contract';
import { confirmDraft, stateQueryKey } from '../api/endpoints';
import { useChat } from '../chat/store';
import { describeError } from '../shared/errors';
import { formatDate, formatTimeRange } from '../shared/format';
import { StatusBadge } from '../shared/StatusBadge';

export function AbsenceDraftCard({ draft }: { draft: AbsenceDraft }) {
  const result = useChat((state) => state.results[draft.draft_id]);
  const saveResult = useChat((state) => state.saveResult);
  const queryClient = useQueryClient();
  // Флаг ставится синхронно: второй клик до перерисовки кнопки не отправит запрос повторно.
  const inFlight = useRef(false);

  const confirm = useMutation({
    mutationFn: () => confirmDraft(draft.draft_id),
    onSuccess: (response) => {
      saveResult(draft.draft_id, response.data);
      void queryClient.invalidateQueries({ queryKey: stateQueryKey });
    },
    onSettled: () => {
      inFlight.current = false;
    },
  });

  function submit() {
    if (inFlight.current || result) return;
    inFlight.current = true;
    confirm.mutate();
  }

  const failure = confirm.isError ? describeError(confirm.error) : null;

  return (
    <article className={result ? 'slip filed' : 'slip'} aria-label="Учебный отгул">
      <div className="slip-head">
        <h3 className="slip-title">Учебный отгул</h3>
        {result ? <StatusBadge status={result.status} /> : <span className="slip-state">Черновик</span>}
      </div>

      <dl className="slip-fields">
        <dt>Дата</dt>
        <dd>{formatDate(draft.date)}</dd>
        <dt>Время</dt>
        <dd>{formatTimeRange(draft.start_time, draft.end_time)}</dd>
      </dl>

      {result ? (
        <>
          <p className="stamp">Заявка №{result.request_number}</p>
          <p className="slip-note">Отправлено руководителю на согласование.</p>
        </>
      ) : (
        <>
          {failure && (
            <p className="slip-error" role="alert">
              {failure.message}
            </p>
          )}
          {(!failure || failure.retryable) && (
            <button
              type="button"
              className="button"
              onClick={submit}
              disabled={confirm.isPending}
              aria-busy={confirm.isPending}
            >
              {confirm.isPending ? 'Отправляем…' : failure ? 'Повторить отправку' : 'Отправить заявку'}
            </button>
          )}
        </>
      )}
    </article>
  );
}
