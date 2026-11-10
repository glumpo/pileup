type Taken = { taken_at: number; local_offset?: number };

export function wallClock(item: Taken): string | undefined {
  const offset = item.local_offset ?? -new Date(item.taken_at).getTimezoneOffset() * 60;
  const date = new Date(item.taken_at + offset * 1000);
  const year = date.getUTCFullYear();
  return year >= 0 && year <= 9999 ? date.toISOString() : undefined;
}

export const dayOf = (item: Taken) => wallClock(item)?.slice(0, 10) ?? 'Дата неизвестна';

export const timeOf = (item: Taken) => wallClock(item)?.slice(11, 16) ?? '';

export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}
