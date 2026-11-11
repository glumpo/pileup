import { act, fireEvent, render, screen, userEvent, within } from '@testing-library/react-native';
import { AppState, AppStateStatus, Dimensions } from 'react-native';

import App from '../App';
import { Pile, TimelineItem } from '../src/api';
import { asset, AUTH, BASE, fakeCore, georgia, Request, sourceOf } from './fakeCore';

const mockRenders: { source: { uri: string }; resizeMode?: string }[] = [];

jest.mock('../src/Preview', () => {
  const actual = jest.requireActual('../src/Preview');
  return {
    ...actual,
    Preview: (props: { source: { uri: string }; resizeMode?: string }) => {
      mockRenders.push(props);
      return actual.Preview(props);
    },
  };
});

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const minute = 60_000;
const thumbhash = '3wcCnxppii9zh4hziEiHl4d/cgiHh4AH';

const gridRenders = () => mockRenders.filter((p) => p.resizeMode !== 'contain').map((p) => p.source.uri);
const cellIds = () => screen.queryAllByTestId(/^asset-/).map((c) => c.props.testID.slice('asset-'.length));
const thumbnail = (id: string) => within(screen.getByTestId(`asset-${id}`)).getByTestId('image');
const wait = (ms: number) => act(() => jest.advanceTimersByTimeAsync(ms));

let listeners: ((state: AppStateStatus) => void)[] = [];

beforeEach(() => {
  mockRenders.length = 0;
  listeners = [];
  jest.useFakeTimers();
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    listeners.push(listener);
    return { remove: () => (listeners = listeners.filter((l) => l !== listener)) };
  });
});

afterEach(() => {
  jest.restoreAllMocks();
  jest.useRealTimers();
});

async function openTimeline(
  timeline: () => TimelineItem[] | Promise<TimelineItem[]>,
  piles: () => Pile[] = () => [georgia],
): Promise<Request[]> {
  const requests = fakeCore({
    'GET /piles': () => [200, piles()],
    'GET /health': [200, { status: 'ok' }],
    'POST /reconnect': [204],
    'GET /piles/aa01/timeline': async () => [200, await timeline()],
  });
  const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
  await render(<App />);
  await user.press(await screen.findByText(piles()[0].name || 'Без названия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await screen.findByTestId('timeline');
  return requests;
}

const many = Array.from({ length: 5000 }, (_, i) =>
  asset(i.toString(16).padStart(4, '0'), i % 2 ? 'Аня' : 'Боря', july1 + i * minute),
);

test('each cell of a scrolled grid is rendered once and its thumbhash is drawn natively', async () => {
  await openTimeline(() => many);
  await screen.findByTestId('asset-0000');
  await wait(500);

  const opened = cellIds();
  expect(gridRenders().length).toBe(opened.length);
  for (const id of opened) {
    expect(thumbnail(id).props.placeholder).toEqual([expect.objectContaining({ uri: expect.stringMatching(/^thumbhash:/) })]);
  }

  const { width, height } = Dimensions.get('window');
  const list = screen.getByTestId('timeline');
  await fireEvent(list, 'layout', { nativeEvent: { layout: { x: 0, y: 0, width, height } } });
  await fireEvent(list, 'contentSizeChange', width, 1e6);
  const seen = new Set(opened);
  mockRenders.length = 0;
  for (let y = 1; y <= 20; y++) {
    await fireEvent.scroll(list, {
      nativeEvent: {
        contentOffset: { x: 0, y: y * height },
        contentSize: { width, height: 1e6 },
        layoutMeasurement: { width, height },
      },
    });
    await wait(50);
  }
  const mounted = cellIds().filter((id) => !seen.has(id));
  expect(mounted.length).toBeGreaterThan(100);
  expect(new Set(gridRenders()).size).toBe(gridRenders().length);
  for (const id of mounted) {
    expect(thumbnail(id).props.placeholder).toEqual([expect.objectContaining({ uri: expect.stringMatching(/^thumbhash:/) })]);
  }
});

test('opening and closing a file does not re-render the cells of the grid', async () => {
  const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
  await openTimeline(() => many.slice(0, 200));
  await screen.findByTestId('asset-0003');
  await wait(500);

  mockRenders.length = 0;
  await user.press(screen.getByTestId('asset-0003'));
  expect(screen.getByText('4 / 200')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '✕' }));
  expect(screen.queryByText('4 / 200')).toBeNull();
  expect(gridRenders()).toEqual([]);
});

const items = [
  asset('f3', 'Аня', july1, { thumbhash: '' }),
  asset('a1', 'Боря', july1 + minute),
  asset('c9', 'Аня', july1 + 2 * minute),
];

test('new files and thumbhashes appear in the open grid by themselves without re-rendering the rest', async () => {
  let timeline = items;
  const requests = await openTimeline(() => timeline);
  const asked = () => requests.filter((r) => r.path === '/piles/aa01/timeline').length;
  await screen.findByTestId('asset-c9');
  expect(thumbnail('f3').props.placeholder ?? []).toEqual([]);
  const before = asked();

  timeline = [{ ...items[0], thumbhash }, ...items.slice(1), asset('07', 'Боря', july1 + 3 * minute)];
  mockRenders.length = 0;
  await wait(9_000);
  expect(asked()).toBe(before);
  await wait(1_500);
  expect(asked()).toBe(before + 1);

  expect(await screen.findByTestId('asset-07')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['f3', 'a1', 'c9', '07']);
  expect(thumbnail('f3').props.placeholder).toEqual([expect.objectContaining({ uri: expect.stringMatching(/^thumbhash:/) })]);
  expect(gridRenders().sort()).toEqual([`${BASE}/piles/aa01/blob/07/r07/1`, `${BASE}/piles/aa01/blob/f3/rf3/1`]);
});

test('the grid is not refreshed in the background and is refreshed at once on return', async () => {
  const requests = await openTimeline(() => items);
  const asked = () => requests.filter((r) => r.path === '/piles/aa01/timeline').length;
  await screen.findByTestId('asset-c9');
  const before = asked();

  await act(async () => listeners.forEach((l) => l('background')));
  await wait(60_000);
  expect(asked()).toBe(before);

  await act(async () => listeners.forEach((l) => l('active')));
  await wait(100);
  expect(asked()).toBe(before + 1);
});

test('a thumbnail that failed to load is loaded again later', async () => {
  await openTimeline(() => items);
  const failed = thumbnail('a1');
  expect(sourceOf(failed)).toEqual({ uri: `${BASE}/piles/aa01/blob/a1/ra1/1`, headers: AUTH });

  await fireEvent(failed, 'error', { nativeEvent: { error: 'HTTP 502' } });
  await wait(30_000);
  expect(thumbnail('a1')).not.toBe(failed);
  expect(sourceOf(thumbnail('a1'))).toEqual({ uri: `${BASE}/piles/aa01/blob/a1/ra1/1`, headers: AUTH });
  const loaded = thumbnail('c9');
  await wait(30_000);
  expect(thumbnail('c9')).toBe(loaded);
});

test('the grid says it is loading until the core answers', async () => {
  let answer: (items: TimelineItem[]) => void = () => {};
  await openTimeline(() => new Promise((r) => (answer = r)));

  expect(screen.getByText('Загружаем хронологию…')).toBeOnTheScreen();
  expect(screen.queryByText(/Пока нет файлов/)).toBeNull();

  await act(async () => answer(items));
  expect(await screen.findByTestId('asset-c9')).toBeOnTheScreen();
  expect(screen.queryByText('Загружаем хронологию…')).toBeNull();
});

test('an empty pile with everyone online says there are no files yet', async () => {
  await openTimeline(() => []);

  expect(await screen.findByText('Пока нет файлов')).toBeOnTheScreen();
});

test('an empty pile names the members who are offline', async () => {
  const boria = { ...georgia.members[1], online: false };
  const dima = { key: '03', name: 'Дима', platform: 'ios', online: false, last_seen: null };
  await openTimeline(
    () => [],
    () => [{ ...georgia, members: [georgia.members[0], boria, dima] }],
  );

  expect(
    await screen.findByText(
      'Пока нет файлов. Нет связи с участниками: Боря, Дима — их файлы появятся, когда они откроют Pileup в этой сети.',
    ),
  ).toBeOnTheScreen();
});

test('a pile that has not synchronised yet says so instead of saying it is empty', async () => {
  await openTimeline(
    () => [],
    () => [{ id: 'aa01', name: '', members: [georgia.members[1]] }],
  );

  expect(
    await screen.findByText(
      'Хронология ещё не получена: не было связи ни с одним участником. Файлы появятся, когда кто-то из них откроет Pileup в этой сети.',
    ),
  ).toBeOnTheScreen();
  expect(screen.queryByText(/Пока нет файлов/)).toBeNull();
});

test('thumbnails are cached in memory only: their addresses change with every start of the core', async () => {
  await openTimeline(() => items);

  expect((await screen.findByTestId('asset-c9')) && thumbnail('c9').props.cachePolicy).toBe('memory');
});
