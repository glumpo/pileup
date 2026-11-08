import { render, screen, userEvent } from '@testing-library/react-native';

import App from '../App';
import { Pile } from '../src/api';
import { AUTH, fakeCore, INVITE } from './fakeCore';

const created: Pile = { id: 'aa01', name: 'Грузия', members: [{ name: 'Аня', platform: 'ios' }] };

test('a created pile opens with its invite as a QR code', async () => {
  const piles: Pile[] = [];
  const requests = fakeCore({
    'GET /piles': () => [200, piles],
    'POST /piles': () => {
      piles.push(created);
      return [201, created];
    },
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
  });
  const user = userEvent.setup();
  await render(<App />);

  await user.type(await screen.findByLabelText('Название поездки'), 'Грузия');
  await user.type(screen.getByLabelText('Ваше имя'), 'Аня');
  await user.press(screen.getByRole('button', { name: 'Создать' }));

  expect(await screen.findByText(INVITE)).toBeOnTheScreen();
  expect(screen.getByRole('image', { name: 'QR-код приглашения' })).toBeOnTheScreen();
  expect(screen.getByText('Грузия')).toBeOnTheScreen();
  expect(screen.getByText('Участники: Аня')).toBeOnTheScreen();
  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/piles', body: { name: 'Грузия', member: 'Аня' } }),
  );
  expect(requests.map((r) => r.headers)).toEqual(requests.map(() => expect.objectContaining(AUTH)));
});

test('existing piles are listed', async () => {
  fakeCore({ 'GET /piles': [200, [created, { id: 'bb02', name: 'Алтай', members: [] }]] });
  await render(<App />);
  expect(await screen.findByText('Грузия')).toBeOnTheScreen();
  expect(screen.getByText('Алтай')).toBeOnTheScreen();
});

test('an invite that cannot be made is explained', async () => {
  fakeCore({ 'GET /piles': [200, [created]], 'GET /piles/aa01/invite': [503, { error: 'offline' }] });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  expect(await screen.findByText('Нет сети: подключитесь к Wi-Fi, чтобы пригласить')).toBeOnTheScreen();
});
