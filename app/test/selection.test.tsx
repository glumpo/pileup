import { fireEvent, render, screen, userEvent } from '@testing-library/react-native';
import { Linking, Platform } from 'react-native';

import App from '../App';
import { Selection } from '../src/api';
import { fakeCore, georgia, INVITE } from './fakeCore';

const native = jest.requireMock('../modules/core/src/PileupCoreModule').default;

const july = (day: number, hour = 0) => new Date(2026, 6, day, hour).getTime();

function core(selection: Selection) {
  return fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/selection': () => [200, selection],
    'PUT /piles/aa01/selection': (body) => {
      selection.from = body.from;
      selection.to = body.to;
      selection.assets = [
        {
          id: 'a1',
          local_id: '1',
          kind: 'photo',
          taken_at: july(3, 12),
          original_name: 'IMG_0001.HEIC',
          screenshot: false,
          hidden: false,
          selected: true,
        },
        {
          id: 'a2',
          local_id: '2',
          kind: 'photo',
          taken_at: july(4, 9),
          original_name: 'IMG_0002.PNG',
          screenshot: true,
          hidden: false,
          selected: false,
        },
      ];
      return [204];
    },
    'PUT /piles/aa01/selection/a1': [204],
  });
}

async function openSelection() {
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Мои файлы' }));
  return user;
}

test('choosing dates lists my files, and a file can be excluded', async () => {
  const requests = core({ from: 0, to: 0, assets: [] });
  const user = await openSelection();

  await user.type(await screen.findByLabelText('С (ГГГГ-ММ-ДД)'), '2026-07-01');
  await user.type(screen.getByLabelText('По (ГГГГ-ММ-ДД)'), '2026-07-10');
  await user.press(screen.getByRole('button', { name: 'Выбрать период' }));

  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'PUT', path: '/piles/aa01/selection', body: { from: july(1), to: july(11) } }),
  );
  expect(await screen.findByText('IMG_0001.HEIC')).toBeOnTheScreen();
  expect(screen.getByText('2026-07-04 09:00 · скриншот')).toBeOnTheScreen();
  expect(screen.getByLabelText('В pile: IMG_0002.PNG').props.value).toBe(false);

  await fireEvent(screen.getByLabelText('В pile: IMG_0001.HEIC'), 'valueChange', false);
  expect(requests).toContainEqual(
    expect.objectContaining({ method: 'PUT', path: '/piles/aa01/selection/a1', body: { selected: false } }),
  );
  expect(screen.getByLabelText('В pile: IMG_0001.HEIC').props.value).toBe(false);
});

test('the saved range is shown again', async () => {
  core({ from: july(1), to: july(11), assets: [] });
  await openSelection();
  expect((await screen.findByLabelText('С (ГГГГ-ММ-ДД)')).props.value).toBe('2026-07-01');
  expect(screen.getByLabelText('По (ГГГГ-ММ-ДД)').props.value).toBe('2026-07-10');
});

test('without access to the media library nothing is requested from the core', async () => {
  native.requestMediaPermissions.mockResolvedValueOnce(false);
  const requests = core({ from: 0, to: 0, assets: [] });
  await openSelection();
  expect(await screen.findByText(/Нет доступа к медиатеке/)).toBeOnTheScreen();
  expect(requests.map((r) => r.path)).not.toContain('/piles/aa01/selection');
});

const settingsHint: [string, string][] = [
  ['android', 'Если окно с вопросом больше не появляется, откройте настройки Pileup, раздел «Разрешения», и разрешите доступ к фото и видео.'],
  ['ios', 'Если окно с вопросом больше не появляется, откройте настройки Pileup, пункт «Фото», и выберите «Полный доступ».'],
];

test.each(settingsHint)('a media library denied on %s explains the way through the app settings', async (os, hint) => {
  jest.replaceProperty(Platform, 'OS', os as typeof Platform.OS);
  const settings = jest.spyOn(Linking, 'openSettings').mockResolvedValue(undefined);
  native.requestMediaPermissions.mockResolvedValue(false);
  core({ from: 0, to: 0, assets: [] });
  const user = await openSelection();

  expect(await screen.findByText(hint)).toBeOnTheScreen();
  expect(screen.getByRole('button', { name: 'Разрешить' })).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'Открыть настройки' }));
  expect(settings).toHaveBeenCalled();
  native.requestMediaPermissions.mockResolvedValue(true);
  jest.restoreAllMocks();
});
