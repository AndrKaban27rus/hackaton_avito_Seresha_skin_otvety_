import { z } from 'zod';

// Форматы ответов бэкенда. Описание для бэкендера: docs/api-absence-scenario.md

const isoDate = z.string().regex(/^\d{4}-\d{2}-\d{2}$/);
const clockTime = z.string().regex(/^\d{2}:\d{2}$/);

export const SourceSchema = z.object({
  source_id: z.string().optional(),
  title: z.string(),
  url: z.string().optional(),
});
export type Source = z.infer<typeof SourceSchema>;

export const AbsenceDraftSchema = z.object({
  draft_id: z.string().min(1),
  expires_at: z.string().optional(),
  date: isoDate,
  start_time: clockTime,
  end_time: clockTime,
});
export type AbsenceDraft = z.infer<typeof AbsenceDraftSchema>;

export type Card =
  | { kind: 'absence_draft'; data: AbsenceDraft }
  | { kind: 'unsupported'; originalKind: string };

const RawCardSchema = z.object({ kind: z.string(), data: z.unknown() });

/**
 * Неизвестный или битый вид карточки не ломает весь ответ чата:
 * текст бота всё равно показывается, а вместо карточки выводится пометка.
 */
export function toCard(raw: z.infer<typeof RawCardSchema> | null | undefined): Card | null {
  if (!raw) return null;
  if (raw.kind === 'absence_draft') {
    const parsed = AbsenceDraftSchema.safeParse(raw.data);
    if (parsed.success) return { kind: 'absence_draft', data: parsed.data };
  }
  return { kind: 'unsupported', originalKind: raw.kind };
}

export const ChatResponseSchema = z.object({
  reply: z.string(),
  sources: z.array(SourceSchema).default([]),
  card: RawCardSchema.nullish().transform(toCard),
});
export type ChatResponse = z.infer<typeof ChatResponseSchema>;

export const AbsenceRequestSchema = z.object({
  request_number: z.union([z.string(), z.number()]).transform(String),
  status: z.string(),
  date: isoDate.optional(),
  start_time: clockTime.optional(),
  end_time: clockTime.optional(),
});
export type AbsenceRequest = z.infer<typeof AbsenceRequestSchema>;

export const ConfirmResponseSchema = z.object({
  kind: z.literal('absence_request'),
  data: AbsenceRequestSchema,
});
export type ConfirmResponse = z.infer<typeof ConfirmResponseSchema>;

export const StateSchema = z.object({
  profile: z
    .object({
      display_name: z.string(),
      grade: z.string().optional(),
      team: z.string().optional(),
    })
    .nullish(),
  scenarios: z
    .object({
      study_absence: z.object({ request: AbsenceRequestSchema.nullish() }).nullish(),
    })
    .default({}),
  next_step: z.string().nullish(),
});
export type OnboardingState = z.infer<typeof StateSchema>;

export const ErrorBodySchema = z.object({
  error: z.union([
    z.string(),
    z.object({ code: z.string(), message: z.string().optional() }),
  ]),
});
