// Разбор фразы стажёра в демо-режиме без бэкенда. Настоящий разбор делает модель на бэкенде.

const MONTHS = [
  'января',
  'февраля',
  'марта',
  'апреля',
  'мая',
  'июня',
  'июля',
  'августа',
  'сентября',
  'октября',
  'ноября',
  'декабря',
];

export interface AbsencePeriod {
  date: string;
  start_time: string;
  end_time: string;
}

export type ParseResult = { ok: true; period: AbsencePeriod } | { ok: false; reason: string } | null;

const pad = (value: number) => String(value).padStart(2, '0');
const isoDay = (day: Date) => `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;

/** null — сообщение не про отсутствие; { ok: false } — про отсутствие, но данных не хватает. */
export function parseAbsenceRequest(text: string, today: Date): ParseResult {
  const lower = text.toLowerCase();
  if (!/(отгул|отсутстви)/.test(lower)) return null;

  const date = findDate(lower, today);
  if (date === null) return { ok: false, reason: 'Укажите дату, например «20 ноября 2030».' };
  if (date === 'invalid') return { ok: false, reason: 'Такой даты нет в календаре. Проверьте день и месяц.' };

  const time = /с\s*(\d{1,2})[:.](\d{2})\s*до\s*(\d{1,2})[:.](\d{2})/.exec(lower);
  if (!time) return { ok: false, reason: 'Укажите время, например «с 14:00 до 18:00».' };

  const [startHour, startMinute, endHour, endMinute] = time.slice(1).map(Number);
  if (startHour > 23 || endHour > 23 || startMinute > 59 || endMinute > 59) {
    return { ok: false, reason: 'Время указано с ошибкой. Используйте формат 14:00.' };
  }
  if (endHour * 60 + endMinute <= startHour * 60 + startMinute) {
    return { ok: false, reason: 'Время окончания должно быть позже времени начала.' };
  }
  if (date < isoDay(today)) return { ok: false, reason: 'Эта дата уже прошла. Укажите будущую дату.' };

  return {
    ok: true,
    period: {
      date,
      start_time: `${pad(startHour)}:${pad(startMinute)}`,
      end_time: `${pad(endHour)}:${pad(endMinute)}`,
    },
  };
}

function findDate(text: string, today: Date): string | 'invalid' | null {
  const iso = /(\d{4})-(\d{2})-(\d{2})/.exec(text);
  const words = new RegExp(`(\\d{1,2})\\s+(${MONTHS.join('|')})(?:\\s+(\\d{4}))?`).exec(text);

  let year: number;
  let month: number;
  let day: number;
  if (iso) {
    [year, month, day] = [Number(iso[1]), Number(iso[2]), Number(iso[3])];
  } else if (words) {
    day = Number(words[1]);
    month = MONTHS.indexOf(words[2]) + 1;
    year = words[3] ? Number(words[3]) : today.getFullYear();
  } else {
    return null;
  }

  const check = new Date(Date.UTC(year, month - 1, day));
  if (check.getUTCFullYear() !== year || check.getUTCMonth() !== month - 1 || check.getUTCDate() !== day) {
    return 'invalid';
  }
  return `${year}-${pad(month)}-${pad(day)}`;
}
