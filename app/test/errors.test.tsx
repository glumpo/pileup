import { fireEvent, render, screen, userEvent, waitFor } from '@testing-library/react-native';

import App from '../App';
import { Selection } from '../src/api';
import { fakeCore, georgia, INVITE, Reply } from './fakeCore';

const native = jest.requireMock('../modules/core/src/PileupCoreModule').default;

const internal =
  'Что-то пошло не так в Pileup. Подробности записаны в «Журнал» на главном экране — отправьте его разработчику.';

test('an unexpected core error says where the details are instead of a code', async () => {
  fakeCore({ 'GET /piles': [500, { error: 'internal' }] });
  await render(<App />);

  expect(await screen.findByText(internal)).toBeOnTheScreen();
  expect(screen.queryByText(/internal/)).toBeNull();
});

const restarting = 'Pileup перезапускается после сбоя: подождите несколько секунд.';

test('a lost connection to the core says that Pileup restarts by itself', async () => {
  globalThis.fetch = jest.fn(async () => {
    throw new TypeError('Network request failed');
  }) as unknown as typeof fetch;
  native.stop.mockReturnValueOnce(new Promise(() => {}));
  await render(<App />);

  expect(await screen.findByText(restarting)).toBeOnTheScreen();
  expect(screen.queryByText(/TypeError|закройте приложение/)).toBeNull();
  await waitFor(() => expect(native.stop).toHaveBeenCalled(), { timeout: 5000 });
});

test('a rejected session restarts Pileup instead of an internal error', async () => {
  fakeCore({ 'GET /piles': [401, ''] });
  native.stop.mockReturnValueOnce(new Promise(() => {}));
  await render(<App />);

  expect(await screen.findByText(restarting)).toBeOnTheScreen();
  expect(screen.queryByText(/internal|закройте приложение/)).toBeNull();
  await waitFor(() => expect(native.stop).toHaveBeenCalled(), { timeout: 5000 });
});

const photo = (id: string, name: string) => ({
  id,
  local_id: id,
  kind: 'photo',
  taken_at: new Date(2026, 6, 3, 12).getTime(),
  original_name: name,
  screenshot: false,
  hidden: false,
  selected: true,
});

async function openSelection(routes: Record<string, Reply | ((body: any) => Reply)>) {
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    ...routes,
  });
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Мои файлы' }));
  return { user, requests };
}

test('a media library the core cannot read is explained in my files', async () => {
  await openSelection({ 'GET /piles/aa01/selection': [500, { error: 'media_failed' }] });

  expect(
    await screen.findByText('Нет доступа к медиатеке. Разрешите доступ к фото и видео в настройках телефона.'),
  ).toBeOnTheScreen();
});

test('a full disk is explained in my files', async () => {
  const { user } = await openSelection({
    'GET /piles/aa01/selection': [200, { from: 0, to: 0, assets: [] }],
    'PUT /piles/aa01/selection': [507, { error: 'disk_full' }],
  });

  await user.type(await screen.findByLabelText('С (ГГГГ-ММ-ДД)'), '2026-07-01');
  await user.type(screen.getByLabelText('По (ГГГГ-ММ-ДД)'), '2026-07-10');
  await user.press(screen.getByRole('button', { name: 'Выбрать период' }));
  expect(await screen.findByText('На телефоне закончилось место. Освободите место и повторите.')).toBeOnTheScreen();
});

test('a file gone from the gallery is explained in my files and the list is refreshed', async () => {
  let selection: Selection = {
    from: new Date(2026, 6, 1).getTime(),
    to: new Date(2026, 6, 11).getTime(),
    assets: [photo('a1', 'IMG_0001.HEIC'), photo('a2', 'IMG_0002.HEIC')],
  };
  await openSelection({
    'GET /piles/aa01/selection': () => [200, selection],
    'PUT /piles/aa01/selection/a1': () => {
      selection = { ...selection, assets: [photo('a2', 'IMG_0002.HEIC')] };
      return [404, { error: 'unknown_asset' }];
    },
  });

  await fireEvent(await screen.findByLabelText('В pile: IMG_0001.HEIC'), 'valueChange', false);
  expect(await screen.findByText('Файла больше нет в галерее — список обновлён.')).toBeOnTheScreen();
  await waitFor(() => expect(screen.queryByText('IMG_0001.HEIC')).toBeNull());
  expect(screen.getByText('IMG_0002.HEIC')).toBeOnTheScreen();
});

test('failed API calls are listed in the log under their own kind', async () => {
  fakeCore({
    'GET /piles': [200, []],
    'GET /diag': [
      200,
      [{ time: 1782900300000, kind: 'api', message: 'POST /piles: disk I/O error', count: 1 }],
    ],
  });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText('POST /piles: disk I/O error')).toBeOnTheScreen();
  expect(screen.getByText(/Ошибка$/)).toBeOnTheScreen();
});
