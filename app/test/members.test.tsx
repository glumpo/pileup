import { act, render, screen, userEvent, waitFor } from '@testing-library/react-native';

import App from '../App';
import { Member } from '../src/api';
import { fakeCore, georgia, INVITE, Reply } from './fakeCore';

const at = (hours: number, minutes: number) => new Date(2026, 10, 10, hours, minutes).getTime();

function core(members: Member[]) {
  fakeCore({
    'GET /piles': () => [200, [{ id: 'aa01', name: 'Грузия', members: members.map((m) => ({ ...m })) }]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, []],
    'GET /piles/aa01/pins': [200, []],
  });
}

async function openPile() {
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
}

test('the pile screen tells for each member whether it is connected and what to do about it', async () => {
  core([
    { key: '01', name: 'Аня', platform: 'ios', online: true, last_seen: at(14, 30) },
    { key: '02', name: 'Боря', platform: 'android', online: false, last_seen: at(14, 5), last_error: 'refused' },
    { key: '03', name: 'Вера', platform: 'ios', online: false, last_seen: at(9, 41), last_error: 'timeout' },
    { key: '04', name: 'Гоша', platform: 'android', online: false, last_seen: null, last_error: 'no_route' },
    { key: '05', name: 'Даша', platform: 'ios', online: false, last_seen: null, last_error: 'other_device' },
    { key: '06', name: 'Егор', platform: 'android', online: false, last_seen: null, last_error: 'no_address' },
    { key: '07', name: 'Жора', platform: 'android', online: false, last_seen: at(8, 0), last_error: 'error' },
  ]);
  await openPile();

  expect(await screen.findByText('Аня — на связи')).toBeOnTheScreen();
  for (const text of [
    'Боря — нет связи с 14:05. Pileup закрыт — попросите открыть Pileup.',
    'Вера — нет связи с 09:41. Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.',
    'Гоша — нет связи с запуска приложения. Телефон не в этой сети — попросите подключиться к той же сети Wi-Fi.',
    'Даша — нет связи с запуска приложения. По известному адресу отвечает другое устройство — попросите открыть Pileup в той же сети Wi-Fi.',
    'Егор — нет связи с запуска приложения. Адрес неизвестен — попросите открыть Pileup в той же сети Wi-Fi.',
    'Жора — нет связи с 08:00. Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.',
  ]) {
    expect(screen.getByText(text)).toBeOnTheScreen();
  }
});

test('the state of members follows their connection while the pile screen is open', async () => {
  const borya: Member = { key: '02', name: 'Боря', platform: 'android', online: true, last_seen: at(14, 5) };
  core([borya]);
  await openPile();
  expect(await screen.findByText('Боря — на связи')).toBeOnTheScreen();

  Object.assign(borya, { online: false, last_error: 'refused' });
  expect(
    await screen.findByText(
      'Боря — нет связи с 14:05. Pileup закрыт — попросите открыть Pileup.',
      {},
      { timeout: 5000 },
    ),
  ).toBeOnTheScreen();
  expect(screen.queryByText('Боря — на связи')).toBeNull();
});

const internal =
  'Что-то пошло не так в Pileup. Подробности записаны в «Журнал» на главном экране — отправьте его разработчику.';
const wait = (ms: number) => act(() => new Promise((r) => setTimeout(r, ms)));

function polled(piles: () => Reply | Promise<Reply>) {
  return fakeCore({
    'GET /piles': piles,
    'GET /piles/aa01/invite': [503, { error: 'offline' }],
    'GET /piles/aa01/timeline': [200, []],
    'GET /piles/aa01/pins': [200, []],
  });
}

test('a failed poll of the pile screen is no longer shown once a poll succeeds', async () => {
  let calls = 0;
  polled(() => (++calls === 3 ? [500, { error: 'internal' }] : [200, [georgia]]));
  await openPile();

  expect(await screen.findByText(internal, {}, { timeout: 5000 })).toBeOnTheScreen();
  await waitFor(() => expect(screen.queryByText(internal)).toBeNull(), { timeout: 5000 });
});

test('the pile screen does not ask again while its poll is still waiting for an answer', async () => {
  let calls = 0;
  const requests = polled(() => (++calls <= 2 ? [200, [georgia]] : new Promise<Reply>(() => {})));
  await openPile();
  await screen.findByText('Аня — на связи');

  await wait(7000);
  expect(requests.filter((r) => r.path === '/piles').length).toBe(3);
});

test('an invite error stays on the pile screen after a successful poll', async () => {
  const requests = polled(() => [200, [georgia]]);
  await openPile();
  const user = userEvent.setup();
  await user.press(await screen.findByRole('button', { name: 'Показать приглашение' }));
  const offline = 'Нет сети: подключитесь к Wi-Fi, чтобы пригласить';
  expect(await screen.findByText(offline)).toBeOnTheScreen();

  const before = requests.filter((r) => r.path === '/piles').length;
  await waitFor(() => expect(requests.filter((r) => r.path === '/piles').length).toBeGreaterThan(before), {
    timeout: 5000,
  });
  await wait(100);
  expect(screen.getByText(offline)).toBeOnTheScreen();
});
