import { render, screen, userEvent, waitFor } from '@testing-library/react-native';
import { Text } from 'react-native';

import App from '../App';
import { connect } from '../src/api';
import { Boundary } from '../src/ui';
import { fakeCore } from './fakeCore';

const reports = (requests: ReturnType<typeof fakeCore>) =>
  requests.filter((r) => r.method === 'POST' && r.path === '/diag').map((r) => r.body);

let previous: jest.Mock;

beforeEach(() => {
  previous = jest.fn();
  ErrorUtils.setGlobalHandler(previous);
});

afterEach(() => jest.restoreAllMocks());

test('an uncaught error is written to the core log and still reaches the default handler', async () => {
  const requests = fakeCore({ 'GET /piles': [200, []], 'POST /diag': [204] });
  await render(<App />);
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();

  const error = new TypeError('undefined is not a function');
  ErrorUtils.getGlobalHandler()(error, false);

  await waitFor(() =>
    expect(reports(requests)).toEqual([
      { kind: 'app', message: expect.stringContaining('TypeError: undefined is not a function') },
    ]),
  );
  expect(previous).toHaveBeenCalledWith(error, false);
});

test('a fatal error is sent before the default handler ends the app', async () => {
  const requests = fakeCore({ 'GET /piles': [200, []], 'POST /diag': [204] });
  await render(<App />);
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();
  let sentBefore = -1;
  previous.mockImplementation(() => (sentBefore = reports(requests).length));

  ErrorUtils.getGlobalHandler()(new Error('render loop'), true);

  await waitFor(() => expect(previous).toHaveBeenCalled());
  expect(sentBefore).toBe(1);
  expect(reports(requests)[0]).toEqual({ kind: 'app', message: expect.stringContaining('render loop') });
});

test('a failing report does not throw', async () => {
  fakeCore({ 'GET /piles': [200, []] });
  globalThis.fetch = jest.fn(() => Promise.reject(new TypeError('Network request failed'))) as unknown as typeof fetch;
  connect(4321, 'test-token');
  expect(() => ErrorUtils.getGlobalHandler()(new Error('boom'), false)).not.toThrow();
  await waitFor(() => expect(previous).toHaveBeenCalled());
});

test('errors go to the core that was started last', async () => {
  const requests = fakeCore({ 'POST /diag': [204] });
  connect(4321, 'old-token');
  const calls = jest.spyOn(globalThis, 'fetch');
  connect(4322, 'new-token');

  ErrorUtils.getGlobalHandler()(new Error('boom'), false);

  await waitFor(() => expect(calls).toHaveBeenCalled());
  expect(calls).toHaveBeenCalledWith(
    'http://127.0.0.1:4322/diag',
    expect.objectContaining({ method: 'POST', headers: expect.objectContaining({ Authorization: 'Bearer new-token' }) }),
  );
  expect(requests).toHaveLength(1);
});

function Broken(): never {
  throw new Error('cannot read property name of undefined');
}

test('an error caught by a boundary is written to the core log', async () => {
  jest.spyOn(console, 'error').mockImplementation(() => {});
  jest.spyOn(console, 'warn').mockImplementation(() => {});
  const requests = fakeCore({ 'POST /diag': [204] });
  connect(4321, 'test-token');

  await render(
    <Boundary item={1} fallback={<Text>Не удалось показать</Text>}>
      <Broken />
    </Boundary>,
  );

  expect(screen.getByText('Не удалось показать')).toBeOnTheScreen();
  await waitFor(() =>
    expect(reports(requests)).toEqual([
      { kind: 'app', message: expect.stringContaining('cannot read property name of undefined') },
    ]),
  );
});

test('the log names app errors and crashes', async () => {
  fakeCore({
    'GET /piles': [200, []],
    'GET /diag': [
      200,
      [
        { time: 1782900300000, kind: 'crash', message: 'previous run crashed: panic: boom', count: 1 },
        { time: 1782900200000, kind: 'app', message: 'TypeError: x', count: 1 },
      ],
    ],
  });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText(/Сбой ядра$/)).toBeOnTheScreen();
  expect(screen.getByText(/Приложение$/)).toBeOnTheScreen();
});
