import { act, render, screen, waitFor } from '@testing-library/react-native';
import { AppState, AppStateStatus } from 'react-native';

import App from '../App';
import { fakeCore } from './fakeCore';

test('returning to the foreground asks the core to retry its connections now', async () => {
  const listeners: ((state: AppStateStatus) => void)[] = [];
  const spy = jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    listeners.push(listener);
    return { remove: jest.fn() };
  });
  const requests = fakeCore({ 'GET /piles': [200, []], 'POST /reconnect': [204] });
  await render(<App />);
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();
  const reconnects = () => requests.filter((r) => r.method === 'POST' && r.path === '/reconnect').length;

  await act(async () => listeners.forEach((l) => l('background')));
  expect(reconnects()).toBe(0);

  await act(async () => listeners.forEach((l) => l('active')));
  await waitFor(() => expect(reconnects()).toBe(1));
  spy.mockRestore();
});
