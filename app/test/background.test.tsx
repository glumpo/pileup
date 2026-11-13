import { act, render, screen, userEvent, waitFor } from '@testing-library/react-native';
import { AppState, AppStateStatus } from 'react-native';

import App from '../App';
import PileupCore from '../modules/core/src/PileupCoreModule';
import { fakeCore, georgia } from './fakeCore';

const core = PileupCore as unknown as { start: jest.Mock; stop: jest.Mock; port: () => number; token: () => string };
let listeners: ((state: AppStateStatus) => void)[] = [];
const appState = (state: AppStateStatus) => act(async () => listeners.forEach((l) => l(state)));
const pause = (ms: number) => act(() => new Promise<void>((resolve) => setTimeout(resolve, ms)));

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

test('a core the phone stopped in the background is restarted only when the app is active again', async () => {
  fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /health': [200, { status: 'ok' }],
    'GET /piles/aa01/invite': [200, { invite: '' }],
    'GET /piles/aa01/timeline': [200, []],
    'GET /piles/aa01/pins': [200, []],
    'GET /serving': [200, { serving: false }],
    'GET /local-network': [200, { denied: false }],
    'GET /show': [404, { error: 'no_show' }],
    'GET /show/interrupted': [404, { error: 'no_show' }],
  });
  const answer = globalThis.fetch;
  let stopped = false;
  globalThis.fetch = jest.fn((url: string, init?: RequestInit) =>
    stopped && url.startsWith('http://127.0.0.1:4321/') ? Promise.reject(new TypeError('Network request failed')) : answer(url, init),
  ) as unknown as typeof fetch;
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await screen.findByRole('button', { name: 'Хронология' });

  await appState('background');
  stopped = true;
  core.port = () => 4322;
  core.token = () => 'new-token';
  await pause(7000);
  expect(core.start).toHaveBeenCalledTimes(1);
  expect(core.stop).not.toHaveBeenCalled();

  await appState('active');
  await waitFor(() => expect(core.start).toHaveBeenCalledTimes(2), { timeout: 10000 });
  expect(core.stop).toHaveBeenCalledTimes(1);
});
