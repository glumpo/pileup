import { act, render, screen, userEvent, waitFor } from '@testing-library/react-native';
import { AppState, AppStateStatus, Share } from 'react-native';

import App from '../App';
import PileupCore from '../modules/core/src/PileupCoreModule';
import { fakeCore, georgia } from './fakeCore';

const core = PileupCore as unknown as {
  start: jest.Mock;
  stop: jest.Mock;
  port: () => number;
  token: () => string;
};

const rejected = (cause: string) =>
  new Error(`Call to function 'PileupCore.start' has been rejected.\n→ Caused by: ${cause}`);

let listeners: ((state: AppStateStatus) => void)[] = [];

beforeEach(() => {
  core.start.mockClear();
  core.stop.mockClear();
  core.port = () => 4321;
  core.token = () => 'test-token';
  listeners = [];
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    listeners.push(listener);
    return { remove: jest.fn() };
  });
});

afterEach(() => jest.restoreAllMocks());

test('a full phone is named on the start failure screen and Повторить starts the core again', async () => {
  fakeCore({ 'GET /piles': [200, []] });
  core.start.mockRejectedValueOnce(rejected('open /data/user/0/core/piles/aa.db: no space left on device'));
  const user = userEvent.setup();
  await render(<App />);

  expect(await screen.findByText(/На телефоне закончилось место/)).toBeOnTheScreen();
  expect(screen.queryByText(/Error:|PileupCore|no space left/)).toBeNull();
  await user.press(screen.getByRole('button', { name: 'Повторить' }));

  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();
  expect(core.start).toHaveBeenCalledTimes(2);
});

test('any other start failure is explained and its details can be shared', async () => {
  fakeCore({ 'GET /piles': [200, []] });
  const error = rejected('open /data/user/0/core/piles.json: permission denied');
  core.start.mockRejectedValueOnce(error);
  const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: Share.sharedAction, activityType: undefined });
  const user = userEvent.setup();
  await render(<App />);

  expect(await screen.findByText(/Не удалось запустить Pileup/)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Повторить' })).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Поделиться подробностями' }));
  expect(share).toHaveBeenCalledWith(expect.objectContaining({ message: expect.stringContaining('permission denied') }));
});

test('an unreadable device key is named and not replaced', async () => {
  fakeCore({ 'GET /piles': [200, []] });
  core.start.mockRejectedValueOnce(rejected('node: device key: seed cannot be decrypted'));
  await render(<App />);

  expect(await screen.findByText(/ключ этого телефона/)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Поделиться подробностями' })).toBeOnTheScreen();
});

test('a core that does not answer on return is restarted and screens use the new port and token', async () => {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /health': () => {
      throw new TypeError('Network request failed');
    },
    'POST /reconnect': [204],
  });
  await render(<App />);
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();

  core.port = () => 4322;
  core.token = () => 'new-token';
  await act(async () => listeners.forEach((l) => l('active')));

  await waitFor(() => expect(core.start).toHaveBeenCalledTimes(2));
  expect(core.stop).toHaveBeenCalledTimes(1);
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({
        url: 'http://127.0.0.1:4322/piles',
        headers: expect.objectContaining({ Authorization: 'Bearer new-token' }),
      }),
    ),
  );
  expect(screen.getByText('Грузия')).toBeOnTheScreen();
});

test('a core that hangs on return is restarted too', async () => {
  fakeCore({ 'GET /piles': [200, []], 'GET /health': () => new Promise(() => {}) });
  await render(<App />);
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();

  await act(async () => listeners.forEach((l) => l('active')));
  await waitFor(() => expect(core.start).toHaveBeenCalledTimes(2), { timeout: 5000 });
});

test('a core that answers on return is kept', async () => {
  fakeCore({ 'GET /piles': [200, []], 'GET /health': [200, { status: 'ok' }], 'POST /reconnect': [204] });
  await render(<App />);
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();

  await act(async () => listeners.forEach((l) => l('active')));
  await act(async () => {});
  expect(core.stop).not.toHaveBeenCalled();
  expect(core.start).toHaveBeenCalledTimes(1);
});

test('a pile that did not open is listed with its reason and the others still open', async () => {
  const requests = fakeCore({
    'GET /piles': [
      200,
      [
        georgia,
        { id: 'bb02', name: 'Армения', members: [], broken: 'corrupt' },
        { id: 'cc03', name: 'Алтай', members: [], broken: 'no_space' },
      ],
    ],
    'GET /piles/aa01/timeline': [200, []],
  });
  const user = userEvent.setup();
  await render(<App />);

  expect(await screen.findByText('Армения')).toBeOnTheScreen();
  expect(screen.getByText(/Не открылся: данные pile на этом телефоне повреждены/)).toBeOnTheScreen();
  expect(screen.getByText(/Не открылся: на телефоне закончилось место/)).toBeOnTheScreen();
  await user.press(screen.getByText('Армения'));
  expect(requests.map((r) => r.path).filter((p) => p.startsWith('/piles/bb02'))).toEqual([]);

  await user.press(screen.getByRole('button', { name: 'Повторить' }));
  await waitFor(() => expect(core.start).toHaveBeenCalledTimes(2));
  expect(core.stop).toHaveBeenCalledTimes(1);

  await user.press(await screen.findByText('Грузия'));
  await waitFor(() => expect(requests.map((r) => r.path)).toContain('/piles/aa01/timeline'));
});

function dies(port: number) {
  const alive = globalThis.fetch;
  let dead = false;
  globalThis.fetch = jest.fn((url: string, init?: RequestInit) =>
    dead && url.startsWith(`http://127.0.0.1:${port}/`)
      ? Promise.reject(new TypeError('Network request failed'))
      : alive(url, init),
  ) as unknown as typeof fetch;
  return () => {
    dead = true;
  };
}

test('a core that stops answering while the app is in use is restarted once by itself', async () => {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /health': [200, { status: 'ok' }],
    'GET /piles/aa01/timeline': [200, []],
    'GET /piles/aa01/pins': [200, []],
  });
  const die = dies(4321);
  const user = userEvent.setup();
  await render(<App />);
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();

  die();
  core.port = () => 4322;
  core.token = () => 'new-token';
  await user.press(screen.getByText('Грузия'));

  await waitFor(() => expect(core.start).toHaveBeenCalledTimes(2));
  await waitFor(() =>
    expect(requests).toContainEqual(
      expect.objectContaining({ url: 'http://127.0.0.1:4322/piles/aa01/timeline' }),
    ),
  );
  expect(await screen.findByText('Аня — на связи')).toBeOnTheScreen();
  await act(() => new Promise((r) => setTimeout(r, 500)));
  expect(core.start).toHaveBeenCalledTimes(2);
  expect(core.stop).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(/закройте приложение|перезапускается/)).toBeNull();
});

test('a failed request to a core that still answers does not restart it', async () => {
  let failures = 1;
  fakeCore({
    'GET /piles': () => {
      if (failures-- > 0) throw new TypeError('Network request failed');
      return [200, [georgia]];
    },
    'GET /health': [200, { status: 'ok' }],
  });
  await render(<App />);
  await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledWith('http://127.0.0.1:4321/health', expect.anything()));
  await act(() => new Promise((r) => setTimeout(r, 200)));
  expect(core.stop).not.toHaveBeenCalled();
  expect(core.start).toHaveBeenCalledTimes(1);
});

test('a core that still does not answer after the restart ends on the start failure screen', async () => {
  globalThis.fetch = jest.fn(async () => {
    throw new TypeError('Network request failed');
  }) as unknown as typeof fetch;
  await render(<App />);

  expect(await screen.findByRole('button', { name: 'Повторить' }, { timeout: 5000 })).toBeOnTheScreen();
  expect(screen.getByText(/Не удалось запустить Pileup/)).toBeOnTheScreen();
  expect(core.start).toHaveBeenCalledTimes(2);
});
