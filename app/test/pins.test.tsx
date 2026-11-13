import { act, render, screen, userEvent, waitFor, within } from '@testing-library/react-native';

import App from '../App';
import { PinEstimate, PinProgress } from '../src/api';
import { asset, fakeCore, georgia, INVITE } from './fakeCore';

const july1 = Date.UTC(2026, 6, 1, 6, 0);

const timeline = [asset('f3', 'Аня', july1), asset('a1', 'Боря', july1 + 1), asset('c9', 'Аня', july1 + 2)];

const anya: PinProgress = { author: '01', name: 'Аня', platform: 'ios', total: 1800, done: 1240, failed: 0 };
const borya: PinProgress = { author: '02', name: 'Боря', platform: 'android', total: 300, done: 0, failed: 0 };

function core(extra: Partial<PinEstimate> = {}, pinned: PinProgress[] = []) {
  const pins = pinned.map((p) => ({ ...p }));
  const estimate = (files: number, missing: number): PinEstimate => ({
    files,
    missing,
    estimated_bytes: missing * 400 * 1024,
    budget_bytes: 1024 ** 3,
    free_bytes: 5.5 * 1024 ** 3,
    over_budget: false,
    ...extra,
  });
  const reply = (): [number, unknown] => [200, pins.map((p) => ({ ...p }))];
  const pin = (...members: PinProgress[]) => {
    for (const m of members) if (!pins.some((p) => p.author === m.author)) pins.push({ ...m });
    return reply();
  };
  const unpin = (author?: string) => () => {
    pins.splice(0, pins.length, ...pins.filter((p) => author && p.author !== author));
    return reply();
  };
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, timeline],
    'GET /piles/aa01/pins': reply,
    'GET /piles/aa01/pins/estimate?member=01': [200, estimate(1800, 560)],
    'GET /piles/aa01/pins/estimate?member=02': [200, estimate(300, 300)],
    'GET /piles/aa01/pins/estimate': [200, estimate(2100, 860)],
    'POST /piles/aa01/pins': (body) => pin(...[anya, borya].filter((m) => !body.member || m.author === body.member)),
    'DELETE /piles/aa01/pins?member=01': unpin('01'),
    'DELETE /piles/aa01/pins?member=02': unpin('02'),
    'DELETE /piles/aa01/pins': unpin(),
  });
  return { requests, pins };
}

async function openPile() {
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await screen.findByRole('button', { name: 'Загрузить всё с участника: Боря' });
  return user;
}

const posts = (requests: { method: string; path: string; body: unknown }[]) =>
  requests.filter((r) => r.method !== 'GET').map((r) => [`${r.method} ${r.path}`, r.body]);

test('all files of a member are loaded from the pile screen after the estimate is confirmed', async () => {
  const { requests } = core();
  const user = await openPile();

  await user.press(screen.getByRole('button', { name: 'Загрузить всё с участника: Аня' }));
  const offer = await screen.findByTestId('pin-offer');
  expect(within(offer).getByText('Загрузить всё с участника: Аня')).toBeOnTheScreen();
  expect(within(offer).getByText('Файлов: 1800, ещё не загружено: 560 (примерно 219 МБ).')).toBeOnTheScreen();
  expect(within(offer).getByText('Свободно на устройстве: 5,5 ГБ.')).toBeOnTheScreen();
  expect(within(offer).queryByText(/Не поместится/)).toBeNull();
  expect(posts(requests)).toEqual([]);

  await user.press(within(offer).getByRole('button', { name: 'Начать загрузку' }));
  expect(await screen.findByText('Аня: 1240/1800')).toBeOnTheScreen();
  expect(screen.queryByTestId('pin-offer')).toBeNull();
  expect(screen.queryByText(/Боря: /)).toBeNull();
  expect(posts(requests)).toEqual([['POST /piles/aa01/pins', { member: '01' }]]);
});

test('everything in the pile is loaded from the pile screen with progress per member', async () => {
  const { requests } = core();
  const user = await openPile();

  await user.press(screen.getByRole('button', { name: 'Загрузить вообще всё' }));
  const offer = await screen.findByTestId('pin-offer');
  expect(within(offer).getByText('Загрузить вообще всё')).toBeOnTheScreen();
  expect(within(offer).getByText('Файлов: 2100, ещё не загружено: 860 (примерно 336 МБ).')).toBeOnTheScreen();
  await user.press(within(offer).getByRole('button', { name: 'Начать загрузку' }));

  expect(await screen.findByText('Аня: 1240/1800')).toBeOnTheScreen();
  expect(screen.getByText('Боря: 0/300')).toBeOnTheScreen();
  expect(requests).toContainEqual(expect.objectContaining({ method: 'GET', path: '/piles/aa01/pins/estimate' }));
  expect(posts(requests)).toEqual([['POST /piles/aa01/pins', {}]]);
});

test('loading over the cache budget is warned about before it starts and can be cancelled', async () => {
  const { requests } = core({ over_budget: true, free_bytes: null });
  const user = await openPile();

  await user.press(screen.getByRole('button', { name: 'Загрузить всё с участника: Боря' }));
  const offer = await screen.findByTestId('pin-offer');
  expect(
    within(offer).getByText(
      'Не поместится в кеш (1 ГБ): закреплённые файлы всё равно загрузятся и займут место сверх него.',
    ),
  ).toBeOnTheScreen();
  expect(within(offer).queryByText(/Свободно/)).toBeNull();

  await user.press(within(offer).getByRole('button', { name: 'Отмена' }));
  expect(screen.queryByTestId('pin-offer')).toBeNull();
  expect(posts(requests)).toEqual([]);
});

test('loading stopped by a full disk on this phone is explained with what to do', async () => {
  core({}, [{ ...anya, done: 1240, failed: 560, reason: 'disk_full' }]);
  await openPile();

  expect(
    await screen.findByText(
      'На этом телефоне закончилось место — загрузка остановлена. Освободите место или снимите закрепления.',
    ),
  ).toBeOnTheScreen();
});

test('loading progress is polled while the pile screen is open', async () => {
  const { requests, pins } = core();
  const user = await openPile();
  await user.press(screen.getByRole('button', { name: 'Загрузить всё с участника: Аня' }));
  await user.press(await screen.findByRole('button', { name: 'Начать загрузку' }));
  await screen.findByText('Аня: 1240/1800');

  pins[0].done = 1500;
  expect(await screen.findByText('Аня: 1500/1800', {}, { timeout: 3000 })).toBeOnTheScreen();
  pins[0].failed = 3;
  expect(await screen.findByText(failed(1500, 3), {}, { timeout: 3000 })).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: '‹ Назад' }));
  await screen.findByRole('button', { name: 'Создать' });
  const polls = () => requests.filter((r) => r.path === '/piles/aa01/pins' && r.method === 'GET').length;
  const before = polls();
  await act(() => new Promise((r) => setTimeout(r, 1500)));
  expect(polls()).toBe(before);
});

const failed = (done: number, count: number) =>
  `Аня: ${done}/1800, не загрузилось: ${count}. Возможно, файлы удалены из галереи или в неподдерживаемом формате — Pileup попробует ещё раз через 5 минут.`;

test('files that did not load are explained and progress is still polled for their retry', async () => {
  const { pins } = core({}, [{ ...anya, done: 1797, failed: 3 }]);
  await openPile();

  expect(await screen.findByText(failed(1797, 3))).toBeOnTheScreen();
  pins[0].done = 1800;
  pins[0].failed = 0;
  expect(await screen.findByText('Аня: 1800/1800', {}, { timeout: 3000 })).toBeOnTheScreen();
});

const waiting: [string, string][] = [
  [
    'unreachable',
    'Боря: 0/300 — ждём: нет связи с участником. Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.',
  ],
  [
    'timeout',
    'Боря: 0/300 — ждём: участник отвечает медленно. Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.',
  ],
  ['video', 'Боря: 0/300 — пауза, пока на экране идёт видео'],
];

test.each(waiting)('loading that waits for a member (%s) says why', async (reason, text) => {
  core({}, [{ ...borya, reason }]);
  await openPile();

  expect(await screen.findByText(text)).toBeOnTheScreen();
});

test('a failed progress poll is no longer shown once a poll succeeds, and polls do not overlap', async () => {
  const { requests } = core({}, [anya]);
  let calls = 0;
  const reply = (globalThis.fetch as jest.Mock).getMockImplementation()!;
  (globalThis.fetch as jest.Mock).mockImplementation(async (url: string, init?: RequestInit) => {
    if (!url.endsWith('/piles/aa01/pins') || (init?.method ?? 'GET') !== 'GET') return reply(url, init);
    calls++;
    if (calls === 2)
      return { ok: false, status: 500, json: async () => ({ error: 'internal' }) } as Response;
    if (calls === 4) return new Promise(() => {});
    return reply(url, init);
  });
  await openPile();

  expect(await screen.findByText(/Что-то пошло не так в Pileup/, {}, { timeout: 3000 })).toBeOnTheScreen();
  await waitFor(() => expect(screen.queryByText(/Что-то пошло не так в Pileup/)).toBeNull(), { timeout: 3000 });
  await act(() => new Promise((r) => setTimeout(r, 2500)));
  expect(calls).toBe(4);
  expect(requests.length).toBeGreaterThan(0);
});

test('pins are removed per member and all at once', async () => {
  const { requests } = core({}, [anya, borya]);
  const user = await openPile();
  expect(await screen.findByText('Аня: 1240/1800')).toBeOnTheScreen();
  expect(screen.getByText('Боря: 0/300')).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Снять закрепление: Аня' }));
  await waitFor(() => expect(screen.queryByText('Аня: 1240/1800')).toBeNull());
  expect(screen.getByText('Боря: 0/300')).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Снять все закрепления' }));
  await waitFor(() => expect(screen.queryByText('Боря: 0/300')).toBeNull());
  expect(screen.queryByRole('button', { name: 'Снять все закрепления' })).toBeNull();
  expect(posts(requests)).toEqual([
    ['DELETE /piles/aa01/pins?member=01', undefined],
    ['DELETE /piles/aa01/pins', undefined],
  ]);
});
