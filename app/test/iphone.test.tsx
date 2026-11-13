import { render, screen, userEvent } from '@testing-library/react-native';
import { BackHandler, Platform, StyleSheet } from 'react-native';

import App from '../App';
import { TimelineItem } from '../src/api';
import { asset, fakeCore, georgia, INVITE, Reply } from './fakeCore';

type ImageProps = { source: { uri: string; headers?: Record<string, string> }; onError?: () => void };

jest.mock('react-native/Libraries/Image/Image', () => {
  const { createElement, useEffect } = require('react');
  const Mock = jest.requireActual('@react-native/jest-preset/jest/mocks/Image').default;
  function Image(props: ImageProps) {
    const { uri, headers } = props.source;
    useEffect(() => {
      if (uri.startsWith('http')) fetch(uri, { headers }).then((r) => !r.ok && props.onError?.());
    }, [uri]);
    return createElement(Mock, props);
  }
  return { __esModule: true, default: Object.assign(Image, Mock) };
});

const safeArea = jest.requireMock('react-native-safe-area-context');
const notch = { top: 59, bottom: 34, left: 0, right: 0 };

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const items: TimelineItem[] = [asset('f3', 'Аня', july1), asset('a1', 'Боря', july1 + 60_000)];

function core(extra: Record<string, Reply> = {}) {
  return fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, items],
    'GET /piles/aa01/selection': [200, { from: 0, to: 0, assets: [] }],
    'GET /piles/aa01/pins': [200, []],
    'GET /screens': [200, [{ id: 'cast-1', name: 'Гостиная', kind: 'cast', address: '192.168.1.20:8009', available: true }]],
    'GET /diag': [200, []],
    ...extra,
  });
}

type Node = { props: { style?: unknown }; parent: Node | null };

function inset(element: Node, side: 'Top' | 'Bottom'): number {
  let sum = 0;
  for (let node: Node | null = element; node; node = node.parent) {
    const s = (StyleSheet.flatten(node.props.style as never) ?? {}) as Record<string, unknown>;
    const pick = (...keys: string[]) => Number(keys.map((k) => s[k]).find((v) => typeof v === 'number') ?? 0);
    sum += pick(`padding${side}`, 'paddingVertical', 'padding') + pick(`margin${side}`, 'marginVertical', 'margin');
  }
  return sum;
}

beforeEach(() => {
  jest.spyOn(BackHandler, 'addEventListener').mockReturnValue({ remove: () => {} });
  jest.replaceProperty(safeArea, 'initialWindowMetrics', { frame: { x: 0, y: 0, width: 402, height: 874 }, insets: notch });
});

afterEach(() => jest.restoreAllMocks());

test('every screen goes back by an on-screen button, without a hardware back key', async () => {
  core();
  const user = userEvent.setup();
  await render(<App />);
  const back = async () => user.press(await screen.findByRole('button', { name: '‹ Назад' }));

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText('Событий нет')).toBeOnTheScreen();
  await back();
  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  expect(await screen.findByRole('button', { name: 'Сканировать QR' })).toBeOnTheScreen();
  await back();

  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await user.press(await screen.findByTestId('asset-a1'));
  expect(screen.getByText('2 / 2')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Закрыть' }));
  expect(screen.queryByText('2 / 2')).toBeNull();
  await back();

  await user.press(await screen.findByRole('button', { name: 'Мои файлы' }));
  expect(await screen.findByText('Выберите период, чтобы увидеть свои фото и видео.')).toBeOnTheScreen();
  await back();
  await user.press(await screen.findByRole('button', { name: 'Показать на экране' }));
  expect(await screen.findByRole('button', { name: 'Гостиная' })).toBeOnTheScreen();
  await back();
  await back();
  expect(await screen.findByRole('button', { name: 'Создать' })).toBeOnTheScreen();
  expect(screen.queryByRole('button', { name: '‹ Назад' })).toBeNull();
});

test('the show remote is left only by stopping the show', async () => {
  const state = {
    pile: 'aa01',
    screen: { id: 'cast-1', name: 'Гостиная', kind: 'cast', address: '192.168.1.20:8009', available: true },
    index: 0,
    total: 2,
    current: { id: 'f3', revision: 'rf3', author: items[0].author, kind: 'photo', taken_at: july1 },
    playing: false,
    waiting: [],
    hints: [],
  };
  core({ 'POST /show': [201, state], 'GET /show': [200, state], 'DELETE /show': [204] });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Показать на экране' }));
  await user.press(await screen.findByRole('button', { name: 'Гостиная' }));

  const stop = await screen.findByRole('button', { name: 'Остановить показ' });
  expect(screen.queryByRole('button', { name: '‹ Назад' })).toBeNull();
  expect(inset(stop, 'Bottom')).toBeGreaterThanOrEqual(notch.bottom);
  await user.press(stop);
  expect(await screen.findByRole('button', { name: 'Хронология' })).toBeOnTheScreen();
});

test('headers stay below the notch and content above the home indicator', async () => {
  core();
  await render(<App />);
  expect(inset(await screen.findByText('Pileup'), 'Top')).toBeGreaterThanOrEqual(notch.top);
  expect(inset(screen.getByRole('button', { name: 'Журнал' }), 'Bottom')).toBeGreaterThanOrEqual(notch.bottom);
});

test('the full-screen viewer keeps its caption below the notch and problems above the home indicator', async () => {
  core({ 'GET /piles/aa01/blob/a1/ra1/2': [409, { error: 'render_failed' }] });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Хронология' }));
  await user.press(await screen.findByTestId('asset-a1'));

  expect(inset(screen.getByRole('button', { name: 'Закрыть' }), 'Top')).toBeGreaterThanOrEqual(notch.top);
  const problem = await screen.findByText(/Не удалось открыть файл/);
  expect(inset(problem, 'Bottom')).toBeGreaterThanOrEqual(notch.bottom);
});

test('a startup failure stays below the notch', async () => {
  const native = jest.requireMock('../modules/core/src/PileupCoreModule').default;
  native.start.mockRejectedValueOnce(new Error('boom'));
  await render(<App />);
  const retry = await screen.findByRole('button', { name: 'Повторить' });
  expect(inset(retry, 'Top')).toBeGreaterThanOrEqual(notch.top);
  expect(inset(retry, 'Bottom')).toBeGreaterThanOrEqual(notch.bottom);
});

test('on iPhone the date fields open a keyboard with digits and a dash', async () => {
  jest.replaceProperty(Platform, 'OS', 'ios');
  core();
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Мои файлы' }));

  expect((await screen.findByLabelText('С (ГГГГ-ММ-ДД)')).props.keyboardType).toBe('numbers-and-punctuation');
  expect(screen.getByLabelText('По (ГГГГ-ММ-ДД)').props.keyboardType).toBe('numbers-and-punctuation');
});
