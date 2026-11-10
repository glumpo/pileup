import { Dimensions } from 'react-native';
import { act, fireEvent, render, screen, userEvent, within } from '@testing-library/react-native';

import App from '../App';
import { TimelineItem } from '../src/api';
import { asset, AUTH, BASE, fakeCore, georgia, INVITE } from './fakeCore';

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const minute = 60_000;
const hour = 60 * minute;

const items: TimelineItem[] = [
  asset('f3', 'Аня', july1, { local_offset: 4 * 3600 }),
  asset('a1', 'Боря', july1 + minute, { kind: 'video', duration_ms: 15_000, local_offset: 4 * 3600 }),
  asset('c9', 'Аня', july1 + 2 * minute, { local_offset: 4 * 3600 }),
  asset('07', 'Боря', july1 + 18 * hour, { local_offset: 4 * 3600 }),
  asset('b2', 'Аня', july1 + 19 * hour, { local_offset: 4 * 3600 }),
];

async function openTimeline(timeline: TimelineItem[]) {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, timeline],
  });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await screen.findByTestId('timeline');
  return { requests, user };
}

const cellIds = () => screen.queryAllByTestId(/^asset-/).map((c) => c.props.testID.slice('asset-'.length));

test('the grid shows files in the order of the core with their authors and day separators', async () => {
  await openTimeline(items);

  expect(await screen.findByTestId('asset-f3')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['f3', 'a1', 'c9', '07', 'b2']);
  expect(screen.getAllByText(/^(2026-07-\d\d|Аня|Боря)$/).map((t) => t.props.children)).toEqual([
    '2026-07-01',
    'Аня',
    'Боря',
    'Аня',
    '2026-07-02',
    'Боря',
    'Аня',
  ]);
  expect(within(screen.getByTestId('asset-a1')).getByText('▶ 0:15')).toBeOnTheScreen();
});

test('a thumbhash is shown until the thumbnail is loaded from the core', async () => {
  await openTimeline(items);
  const cell = await screen.findByTestId('asset-c9');

  expect(within(cell).getByTestId('placeholder').props.source.uri).toMatch(/^data:image\/png;base64,/);
  const thumbnail = within(cell).getByTestId('image');
  expect(thumbnail.props.source).toEqual({ uri: `${BASE}/piles/aa01/blob/c9/rc9/1`, headers: AUTH });

  await fireEvent(thumbnail, 'load');
  expect(within(cell).queryByTestId('placeholder')).toBeNull();
  expect(within(screen.getByTestId('asset-f3')).getByTestId('placeholder')).toBeOnTheScreen();
});

test('a file opens full screen and is swiped forward and back', async () => {
  const { user } = await openTimeline(items);
  const width = Dimensions.get('window').width;
  const swipeTo = (page: number) =>
    fireEvent(screen.getByTestId('viewer'), 'momentumScrollEnd', {
      nativeEvent: { contentOffset: { x: page * width, y: 0 } },
    });

  await user.press(await screen.findByTestId('asset-a1'));
  expect(screen.getByText('2 / 5')).toBeOnTheScreen();
  expect(screen.getByText('Боря')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-01 10:01 · видео 0:15')).toBeOnTheScreen();
  expect(screen.getAllByTestId('image').map((i) => i.props.source)).toContainEqual({
    uri: `${BASE}/piles/aa01/blob/a1/ra1/2`,
    headers: AUTH,
  });

  await swipeTo(2);
  expect(screen.getByText('3 / 5')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-01 10:02')).toBeOnTheScreen();
  expect(screen.getByText('Аня')).toBeOnTheScreen();

  await swipeTo(0);
  expect(screen.getByText('1 / 5')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-01 10:00')).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: '✕' }));
  expect(screen.queryByText('1 / 5')).toBeNull();
  expect(cellIds()).toEqual(['f3', 'a1', 'c9', '07', 'b2']);
});

test('a timeline of 5000 files renders only a window and scrolls to the last file', async () => {
  const many = Array.from({ length: 5000 }, (_, i) =>
    asset(i.toString(16).padStart(4, '0'), i % 3 ? 'Аня' : 'Боря', july1 + i * 7 * minute, {
      kind: i % 10 ? 'photo' : 'video',
      duration_ms: i % 10 ? undefined : 5000,
    }),
  );
  await openTimeline(many);
  await screen.findByTestId('asset-0000');

  const rendered = cellIds();
  expect(rendered.length).toBeGreaterThan(0);
  expect(rendered.length).toBeLessThan(400);
  expect(rendered).toEqual(many.slice(0, rendered.length).map((a) => a.id));

  const { width, height } = Dimensions.get('window');
  const list = screen.getByTestId('timeline');
  await fireEvent(list, 'layout', { nativeEvent: { layout: { x: 0, y: 0, width, height } } });
  await fireEvent(list, 'contentSizeChange', width, 1e7 + height);
  await fireEvent.scroll(list, {
    nativeEvent: {
      contentOffset: { x: 0, y: 1e7 },
      contentSize: { width, height: 1e7 + height },
      layoutMeasurement: { width, height },
    },
  });
  expect(await screen.findByTestId('asset-1387')).toBeOnTheScreen();
  expect(screen.queryByTestId(`asset-${many[2500].id}`)).toBeNull();
  expect(cellIds().length).toBeLessThan(400);
});

const offset = { local_offset: 4 * 3600 };
const impossible = 9e15;
const days = () => screen.getAllByText(/^(2026-07-\d\d|Дата неизвестна|Аня|Боря)$/).map((t) => t.props.children);

test('a file with an impossible date is shown under «Дата неизвестна» and the rest of the grid works', async () => {
  const { user } = await openTimeline([...items, asset('ee', 'Боря', impossible, offset)]);

  expect(await screen.findByTestId('asset-ee')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['f3', 'a1', 'c9', '07', 'b2', 'ee']);
  expect(days()).toEqual(['2026-07-01', 'Аня', 'Боря', 'Аня', '2026-07-02', 'Боря', 'Аня', 'Дата неизвестна', 'Боря']);

  await user.press(screen.getByTestId('asset-07'));
  expect(screen.getByText('4 / 6')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-02 04:00')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '✕' }));

  await user.press(screen.getByTestId('asset-ee'));
  expect(screen.getByText('6 / 6')).toBeOnTheScreen();
  expect(screen.getByText('Дата неизвестна')).toBeOnTheScreen();
});

test('a file with a date before year 0 may come first', async () => {
  await openTimeline([asset('00', 'Аня', -1e14, offset), ...items]);

  expect(await screen.findByTestId('asset-00')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['00', 'f3', 'a1', 'c9', '07', 'b2']);
  expect(days().slice(0, 4)).toEqual(['Дата неизвестна', 'Аня', '2026-07-01', 'Аня']);
});

test('a broken thumbhash leaves a plain placeholder while the thumbnail still loads', async () => {
  const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  await openTimeline(items.map((i) => (i.id === 'c9' ? { ...i, thumbhash: 'не base64' } : i)));
  const cell = await screen.findByTestId('asset-c9');

  expect(within(cell).queryByTestId('placeholder')).toBeNull();
  const thumbnail = within(cell).getByTestId('image');
  expect(thumbnail.props.source).toEqual({ uri: `${BASE}/piles/aa01/blob/c9/rc9/1`, headers: AUTH });
  await fireEvent(thumbnail, 'load');
  expect(within(screen.getByTestId('asset-f3')).getByTestId('placeholder')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['f3', 'a1', 'c9', '07', 'b2']);
  expect(warn).toHaveBeenCalled();
  warn.mockRestore();
});

const unrenderable = (i: TimelineItem) => (i.id === 'c9' ? { ...i, author_name: {} as string } : i);

test('a file that cannot be rendered is replaced by a stub and its neighbours still open', async () => {
  const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  const { user } = await openTimeline(items.map(unrenderable));

  expect(await screen.findByText('Не удалось показать файл')).toBeOnTheScreen();
  expect(cellIds()).toEqual(['f3', 'a1', '07', 'b2']);
  expect(warn).toHaveBeenCalled();
  warn.mockRestore();

  await user.press(screen.getByTestId('asset-a1'));
  expect(screen.getByText('2 / 5')).toBeOnTheScreen();
});

test('broken files are shown normally once the core returns correct data', async () => {
  let timeline = [asset('00', 'Аня', -impossible, offset), ...items.map(unrenderable)];
  timeline[2] = { ...timeline[2], thumbhash: 'не base64' };
  fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/timeline': () => [200, timeline],
  });
  const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  expect(await screen.findByText('Не удалось показать файл')).toBeOnTheScreen();
  warn.mockRestore();

  timeline = [asset('00', 'Аня', july1 - 12 * hour, offset), ...items];
  await act(() => screen.getByTestId('timeline').props.refreshControl.props.onRefresh());

  expect(await screen.findByTestId('asset-c9')).toBeOnTheScreen();
  expect(screen.queryByText('Не удалось показать файл')).toBeNull();
  expect(screen.queryByText('Дата неизвестна')).toBeNull();
  expect(cellIds()).toEqual(['00', 'f3', 'a1', 'c9', '07', 'b2']);
  expect(within(screen.getByTestId('asset-a1')).getByTestId('placeholder')).toBeOnTheScreen();
});

test('in full screen a broken file can be swiped past and closed', async () => {
  const warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
  const broken = [
    ...items.map(unrenderable).map((i) => (i.id === 'a1' ? { ...i, thumbhash: 'не base64' } : i)),
    asset('ee', 'Боря', impossible, offset),
  ];
  const { user } = await openTimeline(broken);
  const width = Dimensions.get('window').width;
  const swipeTo = (page: number) =>
    fireEvent(screen.getByTestId('viewer'), 'momentumScrollEnd', {
      nativeEvent: { contentOffset: { x: page * width, y: 0 } },
    });

  await user.press(await screen.findByTestId('asset-a1'));
  expect(screen.getByText('2 / 6')).toBeOnTheScreen();
  expect(screen.getAllByTestId('image').map((i) => i.props.source)).toContainEqual({
    uri: `${BASE}/piles/aa01/blob/a1/ra1/2`,
    headers: AUTH,
  });

  await swipeTo(2);
  expect(screen.getByText('3 / 6')).toBeOnTheScreen();
  expect(screen.getByText('Не удалось показать подпись')).toBeOnTheScreen();

  await swipeTo(3);
  expect(screen.getByText('4 / 6')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-02 04:00')).toBeOnTheScreen();

  await swipeTo(5);
  expect(screen.getByText('6 / 6')).toBeOnTheScreen();
  expect(screen.getByText('Дата неизвестна')).toBeOnTheScreen();

  await swipeTo(2);
  expect(screen.getByText('3 / 6')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '✕' }));
  expect(screen.queryByText('3 / 6')).toBeNull();
  expect(cellIds()).toEqual(['f3', 'a1', '07', 'b2', 'ee']);
  warn.mockRestore();
});
