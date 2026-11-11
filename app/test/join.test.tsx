import { render, screen, userEvent, waitFor } from '@testing-library/react-native';

import App from '../App';
import { fakeCore, georgia, INVITE, Reply } from './fakeCore';

jest.mock('expo-camera', () => {
  const { createElement } = require('react');
  const { Pressable, Text } = require('react-native');
  return {
    useCameraPermissions: () => [{ granted: true }, async () => ({ granted: true })],
    CameraView: ({ onBarcodeScanned }: { onBarcodeScanned: (r: { type: string; data: string }) => void }) =>
      createElement(
        Pressable,
        { accessibilityRole: 'button', onPress: () => onBarcodeScanned({ type: 'qr', data: mockScanned }) },
        createElement(Text, null, 'QR в кадре'),
      ),
  };
});

let mockScanned = '';

function joinable() {
  const piles: (typeof georgia)[] = [];
  const requests = fakeCore({
    'GET /piles': () => [200, piles],
    'POST /piles/join': () => {
      piles.push(georgia);
      return [201, { id: georgia.id, name: '', members: [] }];
    },
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
  });
  return requests;
}

test('joining by pasted invite text opens the pile', async () => {
  const requests = joinable();
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.paste(screen.getByLabelText('Приглашение'), `  ${INVITE}\n`);
  await user.type(screen.getByLabelText('Ваше имя'), 'Боря');
  await user.press(screen.getByRole('button', { name: 'Войти' }));

  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/piles/join', body: { invite: INVITE, member: 'Боря' } }),
  );
  expect(await screen.findByText('Аня — на связи')).toBeOnTheScreen();
  expect(screen.getByText('Боря — на связи')).toBeOnTheScreen();
  expect(screen.getByText('Грузия')).toBeOnTheScreen();
});

test('scanning the QR code fills the invite', async () => {
  const requests = joinable();
  mockScanned = INVITE;
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.press(screen.getByRole('button', { name: 'Сканировать QR' }));
  await user.press(screen.getByRole('button', { name: 'QR в кадре' }));
  await user.type(screen.getByLabelText('Ваше имя'), 'Боря');
  await user.press(screen.getByRole('button', { name: 'Войти' }));

  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'POST', path: '/piles/join', body: { invite: INVITE, member: 'Боря' } }),
  );
});

test('a QR code that is not an invite is explained and scanning goes on', async () => {
  joinable();
  mockScanned = 'https://example.com';
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.press(screen.getByRole('button', { name: 'Сканировать QR' }));
  await user.press(screen.getByRole('button', { name: 'QR в кадре' }));

  expect(screen.getByLabelText('Приглашение').props.value).toBe('');
  expect(screen.getByRole('button', { name: 'QR в кадре' })).toBeOnTheScreen();
  expect(
    screen.getByText('Это не QR-код приглашения Pileup. Наведите камеру на QR на экране пригласившего.'),
  ).toBeOnTheScreen();
});

test('a rejected invite is explained', async () => {
  fakeCore({ 'GET /piles': [200, []], 'POST /piles/join': [400, { error: 'bad_invite' }] });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.paste(screen.getByLabelText('Приглашение'), 'hello');
  await user.type(screen.getByLabelText('Ваше имя'), 'Боря');
  await user.press(screen.getByRole('button', { name: 'Войти' }));

  expect(await screen.findByText('Это не приглашение Pileup')).toBeOnTheScreen();
});

async function joinWith(reply: Reply | (() => Promise<Reply>), invite = INVITE) {
  fakeCore({ 'GET /piles': [200, []], 'POST /piles/join': typeof reply === 'function' ? reply : () => reply });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.paste(screen.getByLabelText('Приглашение'), invite);
  await user.type(screen.getByLabelText('Ваше имя'), 'Боря');
  await user.press(screen.getByRole('button', { name: 'Войти' }));
}

const connecting = 'Подключаемся к пригласившему — это может занять до 30 секунд.';

test('joining shows that the inviter is being connected to until it ends', async () => {
  let answer: (r: Reply) => void = () => {};
  await joinWith(() => new Promise((r) => (answer = r)));

  expect(await screen.findByText(connecting)).toBeOnTheScreen();
  expect(screen.getByTestId('joining')).toBeOnTheScreen();
  answer([502, { error: 'inviter_unreachable', reason: 'refused' }]);
  await waitFor(() => expect(screen.queryByText(connecting)).toBeNull());
  expect(screen.queryByTestId('joining')).toBeNull();
});

const unreachable: [string | undefined, string][] = [
  ['refused', 'Пригласивший телефон не принимает подключение: попросите открыть Pileup на экране с приглашением.'],
  [
    'timeout',
    'Пригласивший телефон не отвечает. Проверьте, что телефоны в одной сети Wi-Fi. Сеть отеля или гостевая сеть может не пускать телефоны друг к другу — тогда подключитесь к точке доступа с одного из телефонов.',
  ],
  ['no_route', 'Телефоны в разных сетях: подключитесь к той же сети Wi-Fi, что и пригласивший.'],
  ['other_device', 'Приглашение устарело или принадлежит другому pile: попросите показать приглашение ещё раз.'],
  [undefined, 'Не удалось связаться с пригласившим: телефоны должны быть в одной сети'],
];

test.each(unreachable)('an inviter that cannot be reached for the reason %s is explained', async (reason, text) => {
  await joinWith([502, { error: 'inviter_unreachable', reason }]);

  expect(await screen.findByText(text)).toBeOnTheScreen();
});

test('joining from a phone without Wi-Fi says so', async () => {
  await joinWith([503, { error: 'offline' }]);

  expect(
    await screen.findByText('Этот телефон не подключён к Wi-Fi: подключитесь к той же сети, что и пригласивший.'),
  ).toBeOnTheScreen();
});

test('a cut invite is told apart from text that is not an invite', async () => {
  await joinWith([400, { error: 'bad_invite' }], INVITE.slice(0, 40));

  expect(
    await screen.findByText('Приглашение повреждено или скопировано не полностью: скопируйте его ещё раз.'),
  ).toBeOnTheScreen();
});
