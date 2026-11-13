import { act, render, screen, userEvent } from '@testing-library/react-native';
import { Linking } from 'react-native';

import App from '../App';
import { connect } from '../src/api';
import { LocalNetworkBanner } from '../src/LocalNetwork';
import { fakeCore, georgia, INVITE } from './fakeCore';

jest.mock('expo-camera', () => ({
  useCameraPermissions: () => [{ granted: true }, async () => ({ granted: true })],
  CameraView: () => null,
}));

const BANNER =
  'Pileup не может связаться с другими устройствами: запрещён доступ к локальной сети. Откройте Настройки → Pileup и включите «Локальная сеть».';

test('a denied local network is shown on the pile screen with a way to the Settings', async () => {
  const open = jest.spyOn(Linking, 'openSettings').mockResolvedValue();
  fakeCore({ 'GET /piles': [200, [georgia]], 'GET /piles/aa01/timeline': [200, []], 'GET /local-network': [200, { denied: true }] });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByText('Грузия'));
  expect(await screen.findByText(BANNER)).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Открыть Настройки' }));
  expect(open).toHaveBeenCalled();
  open.mockRestore();
});

test('the banner is not shown while the local network is allowed or the core cannot say', async () => {
  for (const reply of [[200, { denied: false }], [404, { error: 'not_found' }]] as const) {
    fakeCore({ 'GET /local-network': [...reply] });
    await render(<LocalNetworkBanner api={connect(4321, 'test-token')} />);
    await act(async () => {});
    expect(screen.queryByText(BANNER)).toBeNull();
  }
});

test('the banner goes away once access is allowed in the Settings', async () => {
  jest.useFakeTimers();
  let denied = true;
  fakeCore({ 'GET /local-network': () => [200, { denied }] });
  await render(<LocalNetworkBanner api={connect(4321, 'test-token')} />);
  expect(await screen.findByText(BANNER)).toBeOnTheScreen();

  denied = false;
  await act(async () => jest.advanceTimersByTime(5000));
  expect(screen.queryByText(BANNER)).toBeNull();
  jest.useRealTimers();
});

test('joining with a denied local network names the cause instead of another network', async () => {
  fakeCore({
    'GET /piles': [200, []],
    'GET /local-network': [200, { denied: true }],
    'POST /piles/join': [502, { error: 'local_network_denied' }],
  });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  expect(await screen.findByText(BANNER)).toBeOnTheScreen();
  await user.paste(screen.getByLabelText('Приглашение'), INVITE);
  await user.type(screen.getByLabelText('Ваше имя'), 'Боря');
  await user.press(screen.getByRole('button', { name: 'Войти' }));

  expect(
    await screen.findByText('Нет доступа к локальной сети: откройте Настройки → Pileup и включите «Локальная сеть»'),
  ).toBeOnTheScreen();
  expect(screen.queryByText(/одной сети/)).toBeNull();
});
