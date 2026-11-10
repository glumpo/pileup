import { Member } from './api';

const advice: Record<string, string> = {
  refused: 'Pileup закрыт — попросите открыть Pileup.',
  no_route: 'Телефон не в этой сети — попросите подключиться к той же сети Wi-Fi.',
  other_device: 'По известному адресу отвечает другое устройство — попросите открыть Pileup в той же сети Wi-Fi.',
  no_address: 'Адрес неизвестен — попросите открыть Pileup в той же сети Wi-Fi.',
};

export function adviceFor(error?: string): string {
  return advice[error ?? ''] ?? 'Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.';
}

const twoDigits = (n: number) => String(n).padStart(2, '0');

export function presenceOf({ name, online, last_seen, last_error }: Member): string {
  if (online) return `${name} — на связи`;
  const seen = last_seen === null ? null : new Date(last_seen);
  const since = seen ? `с ${twoDigits(seen.getHours())}:${twoDigits(seen.getMinutes())}` : 'с запуска приложения';
  return `${name} — нет связи ${since}. ${adviceFor(last_error)}`;
}
