import { render, screen, userEvent } from '@testing-library/react-native';

import App from '../App';
import { Pile } from '../src/api';
import { AUTH, fakeCore, INVITE } from './fakeCore';

const created: Pile = {
  id: 'aa01',
  name: 'Грузия',
  members: [{ key: '01', name: 'Аня', platform: 'ios', online: true, last_seen: Date.UTC(2026, 6, 1, 6, 0) }],
};

const inviteShown = () =>
  [
    screen.queryByText(INVITE),
    screen.queryByRole('image', { name: 'QR-код приглашения' }),
    screen.queryByRole('button', { name: 'Отправить приглашение' }),
  ].some((e) => e !== null);

test('a created pile opens with its invite hidden until it is asked for', async () => {
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

  expect(await screen.findByText('Аня — на связи')).toBeOnTheScreen();
  expect(screen.getByText('Грузия')).toBeOnTheScreen();
  expect(inviteShown()).toBe(false);
  expect(requests.map((r) => r.path)).not.toContain('/piles/aa01/invite');

  await user.press(screen.getByRole('button', { name: 'Показать приглашение' }));
  expect(await screen.findByText(INVITE)).toBeOnTheScreen();
  expect(screen.getByRole('image', { name: 'QR-код приглашения' })).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Отправить приглашение' })).toBeOnTheScreen();
  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/piles', body: { name: 'Грузия', member: 'Аня' } }),
  );
  expect(requests.map((r) => r.headers)).toEqual(requests.map(() => expect.objectContaining(AUTH)));
});

test('a shown invite is hidden again after leaving the pile', async () => {
  fakeCore({
    'GET /piles': [200, [created]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, []],
  });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Показать приглашение' }));
  expect(await screen.findByText(INVITE)).toBeOnTheScreen();

  await user.press(screen.getByRole('button', { name: 'Хронология' }));
  expect(await screen.findByText('Пока нет файлов')).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '‹ Назад' }));
  expect(await screen.findByRole('button', { name: 'Показать приглашение' })).toBeOnTheScreen();
  expect(inviteShown()).toBe(false);

  await user.press(screen.getByRole('button', { name: 'Показать приглашение' }));
  expect(await screen.findByText(INVITE)).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: '‹ Назад' }));
  await user.press(await screen.findByText('Грузия'));
  expect(await screen.findByRole('button', { name: 'Показать приглашение' })).toBeOnTheScreen();
  expect(inviteShown()).toBe(false);
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
  await user.press(await screen.findByRole('button', { name: 'Показать приглашение' }));
  expect(await screen.findByText('Нет сети: подключитесь к Wi-Fi, чтобы пригласить')).toBeOnTheScreen();
});
