import { render, screen, userEvent } from '@testing-library/react-native';

import App from '../App';
import { fakeCore, georgia, INVITE } from './fakeCore';

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
  expect(await screen.findByText('Участники: Аня, Боря')).toBeOnTheScreen();
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

test('a QR code that is not an invite is ignored', async () => {
  joinable();
  mockScanned = 'https://example.com';
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Войти по приглашению' }));
  await user.press(screen.getByRole('button', { name: 'Сканировать QR' }));
  await user.press(screen.getByRole('button', { name: 'QR в кадре' }));

  expect(screen.getByLabelText('Приглашение').props.value).toBe('');
  expect(screen.getByRole('button', { name: 'QR в кадре' })).toBeOnTheScreen();
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
