import { act, render, screen, userEvent, waitFor } from '@testing-library/react-native';
import { AppState, AppStateStatus } from 'react-native';

import App from '../App';
import PileupCore from '../modules/core/src/PileupCoreModule';
import { Screen, ShowState, TimelineItem } from '../src/api';
import { asset, fakeCore, georgia, Reply } from './fakeCore';

jest.mock('expo-keep-awake', () => ({ useKeepAwake: () => {} }));

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const items: TimelineItem[] = [
  asset('f3', 'Аня', july1),
  asset('a1', 'Боря', july1 + 60_000),
  asset('c9', 'Аня', july1 + 120_000),
];

const livingRoom: Screen = {
  id: 'cast-1',
  name: 'Гостиная',
  kind: 'cast',
  address: '192.168.1.20:8009',
  available: true,
};

const interrupted = { pile: 'aa01', screen: livingRoom, asset: 'c9' };

function stateAt(index: number): ShowState {
  const { id, revision, author, kind, taken_at } = items[index];
  return {
    pile: 'aa01',
    screen: livingRoom,
    index,
    total: items.length,
    current: { id, revision, author, kind, taken_at },
    playing: false,
    video_unavailable: false,
    waiting: [],
    hints: [],
  };
}

function core({
  running,
  record = interrupted,
  start = (body: { asset?: string }) => [
    201,
    stateAt(
      Math.max(
        0,
        items.findIndex((i) => i.id === body.asset),
      ),
    ),
  ],
}: {
  running?: ShowState;
  record?: typeof interrupted | null;
  start?: (body: { asset?: string }) => Reply;
} = {}) {
  const core = { running, record, poll: undefined as undefined | (() => Reply) };
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/timeline': [200, items],
    'GET /piles/aa01/pins': [200, []],
    'GET /screens': [200, [livingRoom]],
    'GET /show': () => (core.poll ? core.poll() : core.running ? [200, core.running] : [404, { error: 'no_show' }]),
    'GET /show/interrupted': () => (core.record && !core.running ? [200, core.record] : [404, { error: 'no_show' }]),
    'DELETE /show/interrupted': () => {
      core.record = null;
      return [204];
    },
    'POST /show': (body) => {
      const reply = start(body);
      if (reply[0] === 201) {
        core.running = reply[1] as ShowState;
        core.record = interrupted;
      }
      return reply;
    },
    'DELETE /show': () => {
      core.running = undefined;
      core.record = null;
      return [204];
    },
  });
  return { requests, core };
}

const starts = (requests: { method: string; path: string; body: unknown }[]) =>
  requests.filter((r) => r.method === 'POST' && r.path === '/show').map((r) => r.body);

test('an interrupted show is offered on the list of piles and continues on the same screen from the same file', async () => {
  const { requests } = core();
  const user = userEvent.setup();
  await render(<App />);

  expect(await screen.findByText('Показ на «Гостиная» прервался.')).toBeOnTheScreen();
  expect(screen.getByText('Грузия')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Продолжить' }));

  expect(await screen.findByText('На экране «Гостиная»')).toBeOnTheScreen();
  expect(screen.getByText('3 / 3')).toBeOnTheScreen();
  expect(starts(requests)).toEqual([{ pile: 'aa01', screen: 'cast-1', asset: 'c9' }]);

  await user.press(screen.getByRole('button', { name: 'Остановить показ' }));
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();
  expect(screen.queryByText(/прервался/)).toBeNull();
});

test('a forgotten show is not offered again', async () => {
  const { requests } = core();
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Забыть' }));
  expect(screen.queryByText(/прервался/)).toBeNull();
  expect(requests).toContainEqual(expect.objectContaining({ method: 'DELETE', path: '/show/interrupted' }));
  expect(starts(requests)).toEqual([]);
});

test('nothing is offered without an interrupted show', async () => {
  core({ record: null });
  await render(<App />);
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();
  await act(async () => {});
  expect(screen.queryByText(/прервался/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Продолжить' })).toBeNull();
});

test('a screen that is gone is explained and the usual choice of a screen continues from the same file', async () => {
  let gone = true;
  const { requests } = core({
    start: (body) =>
      gone ? [404, { error: 'unknown_screen' }] : [201, stateAt(items.findIndex((i) => i.id === body.asset))],
  });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Продолжить' }));
  expect(await screen.findByText('Экран больше не найден в сети: обновите список')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Выбрать экран' }));

  expect(await screen.findByText('Показать на экране')).toBeOnTheScreen();
  gone = false;
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));
  expect(await screen.findByText('3 / 3')).toBeOnTheScreen();
  expect(starts(requests).at(-1)).toEqual({ pile: 'aa01', screen: 'cast-1', asset: 'c9' });
});

test('a show that still runs when the app opens returns to its remote', async () => {
  const { requests } = core({ running: stateAt(1) });
  const user = userEvent.setup();
  await render(<App />);

  expect(await screen.findByText('На экране «Гостиная»')).toBeOnTheScreen();
  expect(screen.getByText('2 / 3')).toBeOnTheScreen();
  expect(starts(requests)).toEqual([]);

  await user.press(screen.getByRole('button', { name: 'Остановить показ' }));
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();
});

test('a show lost with a restarted core continues on the open remote by itself, once', async () => {
  const { requests, core: c } = core({ running: stateAt(1) });
  await render(<App />);
  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();

  c.running = undefined;
  c.record = { ...interrupted, asset: 'a1' };
  await act(() => new Promise((r) => setTimeout(r, 1500)));
  expect(starts(requests)).toEqual([{ pile: 'aa01', screen: 'cast-1', asset: 'a1' }]);
  expect(screen.getByText('2 / 3')).toBeOnTheScreen();
  expect(screen.queryByText(/Показ завершён/)).toBeNull();

  c.running = undefined;
  expect(
    await screen.findByText('Показ завершён. Чтобы продолжить, начните показ снова.', {}, { timeout: 10000 }),
  ).toBeOnTheScreen();
  expect(starts(requests)).toHaveLength(1);
});

test('a show that cannot continue on the open remote is explained and a screen can be chosen', async () => {
  const { requests, core: c } = core({ running: stateAt(1), start: () => [502, { error: 'screen_unreachable' }] });
  const user = userEvent.setup();
  await render(<App />);
  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();

  c.running = undefined;
  c.record = { ...interrupted, asset: 'a1' };
  expect(
    await screen.findByText('Показ завершён. Чтобы продолжить, начните показ снова.', {}, { timeout: 10000 }),
  ).toBeOnTheScreen();
  expect(screen.getByText(/Экран не ответил/)).toBeOnTheScreen();
  expect(starts(requests)).toHaveLength(1);

  await user.press(screen.getByRole('button', { name: 'Выбрать экран' }));
  expect(await screen.findByText('Показать на экране')).toBeOnTheScreen();
});

test('after the core is restarted under the open remote the show continues with the new core', async () => {
  const listeners: ((state: AppStateStatus) => void)[] = [];
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    listeners.push(listener);
    return { remove: jest.fn() };
  });
  const native = PileupCore as unknown as { port: () => number; token: () => string };
  const { requests, core: c } = core({ running: stateAt(1) });
  await render(<App />);
  expect(await screen.findByText('2 / 3')).toBeOnTheScreen();

  native.port = () => 4322;
  native.token = () => 'new-token';
  c.running = undefined;
  await act(async () => listeners.forEach((l) => l('active')));

  await waitFor(
    () =>
      expect(requests).toContainEqual(
        expect.objectContaining({
          url: 'http://127.0.0.1:4322/show',
          method: 'POST',
          body: { pile: 'aa01', screen: 'cast-1', asset: 'c9' },
        }),
      ),
    { timeout: 5000 },
  );
  expect(screen.getByText('На экране «Гостиная»')).toBeOnTheScreen();
  expect(screen.queryByText(/Показ завершён/)).toBeNull();
  native.port = () => 4321;
  native.token = () => 'test-token';
  jest.restoreAllMocks();
});
