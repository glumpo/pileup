import { render, screen, userEvent, within } from '@testing-library/react-native';
import { Share } from 'react-native';

import App from '../App';
import { DiagEvent } from '../src/api';
import { fakeCore } from './fakeCore';

const events: DiagEvent[] = [
  { time: 1782900300000, kind: 'timeout', pile: 'aa01', peer: '192.168.1.5:41000', message: 'sync: i/o timeout', count: 3 },
  { time: 1782900200000, kind: 'sync', pile: 'aa01', peer: '192.168.1.5:41000', message: 'sync established', count: 1 },
  { time: 1782900100000, kind: 'renderer', message: 'cast lost', count: 1 },
];

const EXPORT = '2026-07-01T10:15:00.000Z renderer cast lost\n';

test('the log shows core events newest first and exports them through the share sheet', async () => {
  const requests = fakeCore({ 'GET /piles': [200, []], 'GET /diag': [200, events], 'GET /diag/export': [200, EXPORT] });
  const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: Share.sharedAction, activityType: undefined });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText('sync: i/o timeout')).toBeOnTheScreen();
  const messages = screen.getAllByText(/^(sync: i\/o timeout|sync established|cast lost)$/).map((t) => t.props.children);
  expect(messages).toEqual(['sync: i/o timeout', 'sync established', 'cast lost']);
  expect(screen.getByText(/Таймаут · ×3 · 192\.168\.1\.5:41000/)).toBeOnTheScreen();
  expect(screen.getByText(/Синхронизация · 192\.168\.1\.5:41000/)).toBeOnTheScreen();
  expect(screen.getByText(/Экран$/)).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Экспортировать' }));
  expect(share).toHaveBeenCalledWith(expect.objectContaining({ message: EXPORT }));
  expect(requests).toContainEqual(expect.objectContaining({ method: 'GET', path: '/diag/export' }));
});

test('an empty log says so and the back button returns to the piles', async () => {
  fakeCore({ 'GET /piles': [200, []], 'GET /diag': [200, []] });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText('Событий нет')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '‹ Назад' }));
  expect(await screen.findByRole('button', { name: 'Войти по приглашению' })).toBeOnTheScreen();
});
