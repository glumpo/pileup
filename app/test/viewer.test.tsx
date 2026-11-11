import { act, render, screen, userEvent, waitFor, within } from '@testing-library/react-native';

import App from '../App';
import { TimelineItem } from '../src/api';
import { asset, AUTH, BASE, fakeCore, georgia, INVITE, Reply, sourceOf } from './fakeCore';

type ImageProps = {
  source: { uri: string; headers?: Record<string, string> };
  onLoad?: () => void;
  onError?: () => void;
};

jest.mock('react-native/Libraries/Image/Image', () => {
  const { createElement, useEffect } = require('react');
  const Mock = jest.requireActual('@react-native/jest-preset/jest/mocks/Image').default;
  function Image(props: ImageProps) {
    const { uri, headers } = props.source;
    useEffect(() => {
      if (!uri.startsWith('http')) return;
      fetch(uri, { headers }).then(async (r) =>
        r.ok && (await r.text()) === 'JPEG' ? props.onLoad?.() : props.onError?.(),
      );
    }, [uri]);
    return createElement(Mock, props);
  }
  return { __esModule: true, default: Object.assign(Image, Mock) };
});

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const minute = 60_000;

const items: TimelineItem[] = [
  asset('f3', 'Аня', july1),
  asset('a1', 'Боря', july1 + minute),
  asset('c9', 'Аня', july1 + 2 * minute),
  asset('07', 'Боря', july1 + 3 * minute),
];

const jpeg: Reply = [200, 'JPEG'];
const blob = (id: string, tier: 1 | 2) => `/piles/aa01/blob/${id}/r${id}/${tier}`;

async function openViewer(id: string, display: Reply | (() => Reply | Promise<Reply>), list = items) {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, list],
    ...Object.fromEntries(list.flatMap((i) => [`GET ${blob(i.id, 1)}`, `GET ${blob(i.id, 2)}`]).map((r) => [r, jpeg])),
    [`GET ${blob(id, 2)}`]: display,
  });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await user.press(await screen.findByTestId(`asset-${id}`));
  return requests;
}

const viewer = () => within(screen.getByTestId('viewer'));
const sources = () => viewer().getAllByTestId('image').map(sourceOf);

test('a page shows the thumbhash, the thumbnail over it and the display version on top', async () => {
  await openViewer('c9', () => new Promise(() => {}));

  const [thumbnail, display] = viewer().getAllByTestId('image').filter((i) => sourceOf(i).uri.includes('/c9/'));
  expect(thumbnail.props.placeholder).toEqual([expect.objectContaining({ uri: expect.stringMatching(/^thumbhash:/) })]);
  expect(sourceOf(thumbnail)).toEqual({ uri: BASE + blob('c9', 1), headers: AUTH });
  expect(sourceOf(display)).toEqual({ uri: BASE + blob('c9', 2), headers: AUTH });
});

const asleep = 'Нет ответа от участника: Боря. Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.';

test('a page of a member who does not answer says so and loads by itself once the member is back', async () => {
  let display: Reply = [502, { error: 'source_unavailable' }];
  const requests = await openViewer('07', () => display);
  const asked = () => requests.filter((r) => r.path === blob('07', 2)).length;

  expect(await screen.findByText(asleep)).toBeOnTheScreen();
  expect(sources()).toContainEqual({ uri: BASE + blob('07', 1), headers: AUTH });
  const before = asked();
  await waitFor(() => expect(asked()).toBeGreaterThan(before), { timeout: 5000 });
  expect(screen.getByText(asleep)).toBeOnTheScreen();

  display = jpeg;
  await waitFor(() => expect(screen.queryByText(asleep)).toBeNull(), { timeout: 5000 });
  const loaded = asked();
  await act(() => new Promise((r) => setTimeout(r, 2500)));
  expect(asked()).toBe(loaded);
  expect(screen.queryByText(asleep)).toBeNull();
});

const failures: [string, Reply, string][] = [
  ['the file was removed', [404, { error: 'unknown_asset' }], 'Файл удалён или изменён: обновите хронологию'],
  [
    'the core failed',
    [500, { error: 'internal' }],
    'Не удалось загрузить файл. Что-то пошло не так в Pileup. Подробности записаны в «Журнал» на главном экране — отправьте его разработчику.',
  ],
  [
    'the disk is full',
    [507, { error: 'disk_full' }],
    'Не удалось загрузить файл. На телефоне закончилось место. Освободите место и повторите.',
  ],
  [
    'an own file cannot be rendered',
    [500, { error: 'render_failed' }],
    'Не удалось открыть файл на этом телефоне. Возможно, он удалён из галереи или в неподдерживаемом формате.',
  ],
  ['the file cannot be shown', [200, 'HTML'], 'Не удалось показать файл'],
];

test('a file the member phone could not prepare is explained and not asked for again', async () => {
  const requests = await openViewer('07', [502, { error: 'source_failed' }]);
  const asked = () => requests.filter((r) => r.path === blob('07', 2)).length;

  expect(
    await screen.findByText(
      'Телефон участника не смог подготовить файл: Боря. Возможно, файл удалён из галереи или в неподдерживаемом формате.',
    ),
  ).toBeOnTheScreen();
  const before = asked();
  await act(() => new Promise((r) => setTimeout(r, 2500)));
  expect(asked()).toBe(before);
});

test.each(failures)('a page says so when %s', async (_, display, text) => {
  await openViewer('07', display);

  expect(await screen.findByText(text)).toBeOnTheScreen();
});

const unavailable: [string, string][] = [
  ['refused', 'Нет ответа от участника: Боря. Pileup закрыт — попросите открыть Pileup.'],
  ['no_route', 'Нет ответа от участника: Боря. Телефон не в этой сети — попросите подключиться к той же сети Wi-Fi.'],
  ['timeout', asleep],
];

test.each(unavailable)('a page of a member who does not answer gives the advice for %s', async (reason, text) => {
  await openViewer('07', [502, { error: 'source_unavailable', reason }]);

  expect(await screen.findByText(text)).toBeOnTheScreen();
});

test('a page shows that the file is loading until it is shown', async () => {
  await openViewer('c9', () => new Promise(() => {}));
  await waitFor(() => expect(viewer().getAllByTestId('loading')).toHaveLength(1));
});

test('a page shows no loading indicator once the file is shown or has failed', async () => {
  await openViewer('07', [502, { error: 'source_unavailable', reason: 'refused' }]);

  await screen.findByText('Нет ответа от участника: Боря. Pileup закрыт — попросите открыть Pileup.');
  await waitFor(() => expect(viewer().queryAllByTestId('loading')).toEqual([]));
});

test('a page that failed to show although the file is there is loaded once more', async () => {
  let calls = 0;
  const requests = await openViewer('07', () => (++calls === 1 ? [200, 'HTML'] : jpeg));
  const asked = () => requests.filter((r) => r.path === blob('07', 2)).length;

  await waitFor(() => expect(asked()).toBe(3));
  await waitFor(() => expect(viewer().queryAllByTestId('loading')).toEqual([]));
  expect(screen.queryByText('Не удалось показать файл')).toBeNull();
});

test('a video page is marked and says how to watch it', async () => {
  await openViewer('v1', () => new Promise(() => {}), [
    ...items,
    asset('v1', 'Аня', july1 + 4 * minute, { kind: 'video', duration_ms: 32_000 }),
  ]);

  expect(await screen.findByTestId('video')).toBeOnTheScreen();
  expect(screen.getByText('Видео воспроизводится на экране: нажмите «На экран».')).toBeOnTheScreen();
});

test('a photo page has no video mark', async () => {
  await openViewer('c9', () => new Promise(() => {}));

  await waitFor(() => expect(viewer().getAllByTestId('loading').length).toBeGreaterThan(0));
  expect(screen.queryByTestId('video')).toBeNull();
  expect(screen.queryByText(/Видео воспроизводится/)).toBeNull();
});

test('two neighbouring pages on each side are loaded in advance', async () => {
  const many = Array.from({ length: 9 }, (_, i) => asset(`p${i}`, 'Аня', july1 + i * minute));
  const requests = await openViewer('p4', () => new Promise(() => {}), many);

  await waitFor(() =>
    expect(new Set(requests.filter((r) => r.path.endsWith('/2')).map((r) => r.path))).toEqual(
      new Set(['p2', 'p3', 'p4', 'p5', 'p6'].map((id) => blob(id, 2))),
    ),
  );
  await act(() => new Promise((r) => setTimeout(r, 300)));
  expect(new Set(requests.filter((r) => r.path.endsWith('/2')).map((r) => r.path)).size).toBe(5);
});
