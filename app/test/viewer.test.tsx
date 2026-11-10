import { act, render, screen, userEvent, waitFor, within } from '@testing-library/react-native';

import App from '../App';
import { TimelineItem } from '../src/api';
import { asset, AUTH, BASE, fakeCore, georgia, INVITE, Reply } from './fakeCore';

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

async function openViewer(id: string, display: Reply | (() => Reply | Promise<Reply>)) {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, items],
    ...Object.fromEntries(items.flatMap((i) => [`GET ${blob(i.id, 1)}`, `GET ${blob(i.id, 2)}`]).map((r) => [r, jpeg])),
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
const sources = () => viewer().getAllByTestId('image').map((i) => i.props.source);

test('a page shows the thumbnail instead of the thumbhash while the display version loads', async () => {
  await openViewer('c9', () => new Promise(() => {}));

  await waitFor(() => expect(viewer().queryAllByTestId('placeholder')).toEqual([]));
  expect(sources()).toContainEqual({ uri: BASE + blob('c9', 1), headers: AUTH });
  expect(sources()).toContainEqual({ uri: BASE + blob('c9', 2), headers: AUTH });
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
  ['the core failed', [500, { error: 'internal' }], 'Не удалось загрузить файл. Ошибка ядра: internal'],
  ['the file cannot be shown', [200, 'HTML'], 'Не удалось показать файл'],
];

test.each(failures)('a page says so when %s', async (_, display, text) => {
  await openViewer('07', display);

  expect(await screen.findByText(text)).toBeOnTheScreen();
});
