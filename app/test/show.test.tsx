import { act, render, screen, userEvent, within } from '@testing-library/react-native';

import App from '../App';
import { Hint, PinEstimate, PinProgress, Screen, ShowState, TimelineItem } from '../src/api';
import { asset, AUTH, BASE, fakeCore, georgia, INVITE } from './fakeCore';

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const minute = 60_000;

const items: TimelineItem[] = [
  asset('f3', 'Аня', july1, { local_offset: 4 * 3600 }),
  asset('a1', 'Боря', july1 + minute, { kind: 'video', duration_ms: 15_000, local_offset: 4 * 3600 }),
  asset('c9', 'Аня', july1 + 2 * minute, { local_offset: 4 * 3600 }),
];

const livingRoom: Screen = {
  id: 'cast-1',
  name: 'Гостиная',
  kind: 'cast',
  address: '192.168.1.20:8009',
  available: true,
};

const appleReason = 'Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника.';

const bedroom: Screen = {
  id: 'airplay-1',
  name: 'Спальня',
  kind: 'airplay',
  address: '192.168.1.30:7000',
  available: false,
  reason: appleReason,
};

const showItem = ({ id, revision, author, kind, taken_at }: TimelineItem) => ({ id, revision, author, kind, taken_at });

const estimate: PinEstimate = {
  files: 2,
  missing: 1,
  estimated_bytes: 400 * 1024,
  budget_bytes: 1024 ** 3,
  free_bytes: 12 * 1024 ** 3,
  over_budget: false,
};

function core(screens: Screen[] = [livingRoom, bedroom], start?: [number, unknown]) {
  const show = {
    index: 0,
    playing: false,
    renderer_error: undefined as ShowState['renderer_error'],
    waiting: [] as TimelineItem[],
    hints: [] as Hint[],
    videoUnavailable: false,
  };
  const state = (): ShowState => ({
    pile: 'aa01',
    screen: livingRoom,
    index: show.index,
    total: items.length,
    current: showItem(items[show.index]),
    playing: show.playing,
    video_unavailable: show.videoUnavailable && items[show.index].kind === 'video',
    renderer_error: show.renderer_error,
    waiting: show.waiting.map(showItem),
    hints: show.hints,
  });
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, items],
    'GET /screens': () => [200, screens],
    'POST /show': (body) => {
      if (start) return start;
      show.index = Math.max(
        0,
        items.findIndex((i) => i.id === body.asset),
      );
      show.playing = items[show.index].kind === 'video';
      show.renderer_error = undefined;
      return [201, state()];
    },
    'GET /show': () => [200, state()],
    'POST /show/next': () => {
      show.index = Math.min(show.index + 1, items.length - 1);
      show.playing = items[show.index].kind === 'video';
      return [200, state()];
    },
    'POST /show/prev': () => {
      show.index = Math.max(show.index - 1, 0);
      show.playing = items[show.index].kind === 'video';
      return [200, state()];
    },
    'POST /show/play': () => {
      if (items[show.index].kind !== 'video') return [409, { error: 'not_video' }];
      show.playing = true;
      return [200, state()];
    },
    'POST /show/pause': () => {
      if (items[show.index].kind !== 'video') return [409, { error: 'not_video' }];
      show.playing = false;
      return [200, state()];
    },
    'DELETE /show': [204],
    'GET /piles/aa01/pins': [200, [] as PinProgress[]],
    'GET /piles/aa01/pins/estimate?member=01': [200, estimate],
    'GET /piles/aa01/pins/estimate': [200, { ...estimate, files: 3, missing: 2, estimated_bytes: 800 * 1024 }],
    'POST /piles/aa01/pins': [200, [{ author: '01', name: 'Аня', platform: 'ios', total: 2, done: 0, failed: 0 }]],
  });
  return { requests, show };
}

async function openScreens() {
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Показать на экране' }));
  return user;
}

async function startShow() {
  const user = await openScreens();
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));
  await screen.findByText('На экране «Гостиная»');
  return user;
}

const commands = (requests: { method: string; path: string }[]) =>
  requests.filter((r) => r.method !== 'GET').map((r) => `${r.method} ${r.path}`);

test('found screens are listed, and an Apple TV that cannot be used is shown with its reason', async () => {
  const screens = [livingRoom];
  core(screens);
  await openScreens();

  expect(await screen.findByRole('button', { name: 'Гостиная' })).toBeOnTheScreen();
  expect(screen.queryByText('Спальня')).toBeNull();

  screens.push(bedroom);
  await act(() => screen.getByTestId('screens').props.refreshControl.props.onRefresh());

  const unavailable = await screen.findByTestId('unavailable-airplay-1');
  expect(within(unavailable).getByText('Спальня')).toBeOnTheScreen();
  expect(within(unavailable).getByText(appleReason)).toBeOnTheScreen();
  expect(screen.queryByRole('button', { name: 'Спальня' })).toBeNull();
});

test('without screens in the network the picker says what to check', async () => {
  core([]);
  await openScreens();
  expect(await screen.findByText(/Экраны не найдены/)).toBeOnTheScreen();
});

test('a show started from the pile runs on the chosen screen and the remote shows its first file', async () => {
  const { requests } = core();
  await startShow();

  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/show', body: { pile: 'aa01', screen: 'cast-1' } }),
  );
  expect(screen.getByText('1 / 3')).toBeOnTheScreen();
  expect(await screen.findByText('Аня')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-01 10:00')).toBeOnTheScreen();
  expect(screen.getByTestId('image').props.source).toEqual({ uri: `${BASE}/piles/aa01/blob/f3/rf3/2`, headers: AUTH });
});

test('a show started from the full-screen viewer starts at the opened file', async () => {
  const { requests } = core();
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await user.press(await screen.findByTestId('asset-a1'));
  await user.press(screen.getByRole('button', { name: 'Показать на экране' }));
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));

  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();
  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/show', body: { pile: 'aa01', screen: 'cast-1', asset: 'a1' } }),
  );
});

test('next and previous move the show and the remote follows', async () => {
  const { requests } = core();
  const user = await startShow();

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();
  expect(await screen.findByText('Боря')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-01 10:01 · видео')).toBeOnTheScreen();
  expect(screen.getByTestId('image').props.source.uri).toBe(`${BASE}/piles/aa01/blob/a1/ra1/2`);

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  expect(await screen.findByText('3 / 3')).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Предыдущий' }));
  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();
  expect(commands(requests)).toEqual(['POST /show', 'POST /show/next', 'POST /show/next', 'POST /show/prev']);
});

test('play and pause are offered only for a video', async () => {
  const { requests } = core();
  const user = await startShow();

  expect(screen.queryByRole('button', { name: 'Пауза' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Воспроизвести' })).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  await user.press(await screen.findByRole('button', { name: 'Пауза' }));
  await user.press(await screen.findByRole('button', { name: 'Воспроизвести' }));
  expect(await screen.findByRole('button', { name: 'Пауза' })).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  expect(await screen.findByText('3 / 3')).toBeOnTheScreen();
  expect(screen.queryByRole('button', { name: 'Пауза' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Воспроизвести' })).toBeNull();
  expect(commands(requests)).toEqual([
    'POST /show',
    'POST /show/next',
    'POST /show/pause',
    'POST /show/play',
    'POST /show/next',
  ]);
});

test('a screen that became unavailable is explained with its reason as is', async () => {
  core([livingRoom], [409, { error: 'screen_unavailable', reason: 'Экран «Гостиная» занят другим приложением.' }]);
  const user = await openScreens();
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));

  expect(await screen.findByText('Экран «Гостиная» занят другим приложением.')).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Гостиная' })).toBeOnTheScreen();
});

test('a screen that does not answer is reported', async () => {
  core([livingRoom], [502, { error: 'screen_unreachable' }]);
  const user = await openScreens();
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));

  expect(await screen.findByText(/Экран не ответил/)).toBeOnTheScreen();
});

const advice = 'Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.';
const anyaAway: Hint = { author: '01', name: 'Аня', platform: 'ios', reason: 'unreachable' };

test('members the show waits for are named with their phone and why', async () => {
  const { show } = core();
  await startShow();
  expect(screen.queryByTestId('waiting')).toBeNull();

  show.waiting = [items[2], items[1]];
  show.hints = [anyaAway, { author: '02', name: 'Боря', platform: 'android', reason: 'timeout' }];
  const banner = await screen.findByTestId('waiting', {}, { timeout: 3000 });
  expect(within(banner).getByText(`Нет ответа от iPhone: Аня. ${advice}`)).toBeOnTheScreen();
  expect(within(banner).getByText(`Медленно отвечает телефон: Боря. ${advice}`)).toBeOnTheScreen();
  expect(within(banner).getByRole('button', { name: 'Загрузить всё с участника: Аня' })).toBeOnTheScreen();
  expect(within(banner).getByRole('button', { name: 'Загрузить всё с участника: Боря' })).toBeOnTheScreen();

  show.waiting = [];
  show.hints = [];
  await act(() => new Promise((r) => setTimeout(r, 1500)));
  expect(screen.queryByTestId('waiting')).toBeNull();
});

test('all files of a member who does not answer are loaded from the remote after the estimate is confirmed', async () => {
  const { requests, show } = core();
  const user = await startShow();
  show.hints = [anyaAway];
  const banner = await screen.findByTestId('waiting', {}, { timeout: 3000 });

  await user.press(within(banner).getByRole('button', { name: 'Загрузить всё с участника: Аня' }));
  const offer = await screen.findByTestId('pin-offer');
  expect(within(offer).getByText('Загрузить всё с участника: Аня')).toBeOnTheScreen();
  expect(within(offer).getByText('Файлов: 2, ещё не загружено: 1 (примерно 400 КБ).')).toBeOnTheScreen();
  expect(within(offer).getByText('Свободно на устройстве: 12 ГБ.')).toBeOnTheScreen();
  expect(requests).toContainEqual(expect.objectContaining({ method: 'GET', path: '/piles/aa01/pins/estimate?member=01' }));
  expect(commands(requests)).not.toContain('POST /piles/aa01/pins');

  await user.press(within(offer).getByRole('button', { name: 'Начать загрузку' }));
  expect(await screen.findByText('Аня: 0/2')).toBeOnTheScreen();
  expect(screen.queryByTestId('pin-offer')).toBeNull();
  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/piles/aa01/pins', body: { member: '01' } }),
  );
});

test('everything in the pile can be loaded from the remote', async () => {
  const { requests } = core();
  const user = await startShow();

  await user.press(screen.getByRole('button', { name: 'Загрузить вообще всё' }));
  const offer = await screen.findByTestId('pin-offer');
  expect(within(offer).getByText('Файлов: 3, ещё не загружено: 2 (примерно 800 КБ).')).toBeOnTheScreen();
  await user.press(within(offer).getByRole('button', { name: 'Начать загрузку' }));

  expect(await screen.findByText('Аня: 0/2')).toBeOnTheScreen();
  expect(requests).toContainEqual(expect.objectContaining({ method: 'GET', path: '/piles/aa01/pins/estimate' }));
  expect(requests).toContainEqual(expect.objectContaining({ method: 'POST', path: '/piles/aa01/pins', body: {} }));
});

test('an unavailable video is explained over its poster and the show goes on', async () => {
  const { requests, show } = core();
  const user = await startShow();
  show.videoUnavailable = true;
  expect(screen.queryByText(/Видео недоступно/)).toBeNull();

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  expect(await screen.findByText(/Видео недоступно/)).toBeOnTheScreen();
  expect(screen.getByText('2 / 3')).toBeOnTheScreen();
  expect(screen.getByTestId('image').props.source.uri).toBe(`${BASE}/piles/aa01/blob/a1/ra1/2`);

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  expect(await screen.findByText('3 / 3')).toBeOnTheScreen();
  expect(screen.queryByText(/Видео недоступно/)).toBeNull();
  expect(commands(requests)).toEqual(['POST /show', 'POST /show/next', 'POST /show/next']);
});

test('screen errors are explained in Russian by their code', async () => {
  const { show } = core();
  await startShow();

  show.renderer_error = { code: 'load_failed', message: 'LOAD_FAILED from receiver' };
  expect(await screen.findByText(/Экран не смог открыть этот файл/, {}, { timeout: 3000 })).toBeOnTheScreen();
  expect(screen.queryByText(/LOAD_FAILED/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Перезапустить показ' })).toBeNull();
});

test('after the screen disconnects the show is restarted from the current file', async () => {
  const { requests, show } = core();
  const user = await startShow();
  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  await screen.findByText('2 / 3');

  show.renderer_error = { code: 'disconnected', message: 'cast: connection closed' };
  expect(await screen.findByText('Связь с экраном потеряна.', {}, { timeout: 3000 })).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Перезапустить показ' }));

  expect(requests.filter((r) => r.method === 'POST' && r.path === '/show').pop()?.body).toEqual({
    pile: 'aa01',
    screen: 'cast-1',
    asset: 'a1',
  });
  expect(screen.queryByText('Связь с экраном потеряна.')).toBeNull();
  expect(screen.getByText('2 / 3')).toBeOnTheScreen();
});

test('stopping the show returns to the pile', async () => {
  const { requests } = core();
  const user = await startShow();

  await user.press(screen.getByRole('button', { name: 'Остановить показ' }));

  expect(await screen.findByRole('button', { name: 'Хронология' })).toBeOnTheScreen();
  expect(screen.queryByText('На экране «Гостиная»')).toBeNull();
  expect(commands(requests)).toEqual(['POST /show', 'DELETE /show']);
});
