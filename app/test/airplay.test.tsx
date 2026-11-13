import { act, render, screen, userEvent, within } from '@testing-library/react-native';
import { Platform } from 'react-native';

import App from '../App';
import { Screen, ShowState, TimelineItem } from '../src/api';
import { asset, fakeCore, georgia, INVITE, Reply } from './fakeCore';

const native = jest.requireMock('../modules/core/src/PileupCoreModule').default;

const july1 = Date.UTC(2026, 6, 1, 6, 0);
const items: TimelineItem[] = [
  asset('f3', 'Аня', july1),
  asset('a1', 'Боря', july1 + 60_000, { kind: 'video', duration_ms: 15_000 }),
  asset('c9', 'Аня', july1 + 120_000),
];

const bedroom: Screen = { id: 'airplay:Спальня', name: 'Спальня', kind: 'airplay', address: '192.168.1.30:7000', available: true };
const livingRoom: Screen = { id: 'cast-1', name: 'Гостиная', kind: 'cast', address: '192.168.1.20:8009', available: true };

const port = 'http://192.168.1.5:50123/show/tok';
const t2 = (i: TimelineItem) => `${port}/${i.id}/${i.revision}/2`;

function core(screens: Screen[], extra: Record<string, Reply | ((body: any) => Reply | Promise<Reply>)> = {}) {
  const show = { index: 0, playing: false, screen: bedroom, running: false };
  const state = (): ShowState => {
    const i = items[show.index];
    return {
      pile: 'aa01',
      screen: show.screen,
      index: show.index,
      total: items.length,
      current: { id: i.id, revision: i.revision, author: i.author, kind: i.kind, taken_at: i.taken_at },
      playing: show.playing,
      video_unavailable: false,
      waiting: [],
      hints: [],
      ...(show.screen.kind === 'airplay' && {
        slide: {
          image: t2(i),
          ...(i.kind === 'video' && { video: `${port}/${i.id}/${i.revision}/3` }),
          around: [items[show.index + 1], items[show.index - 1]].filter(Boolean).map(t2),
        },
      }),
    };
  };
  const requests = fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, items],
    'GET /piles/aa01/pins': [200, []],
    'GET /screens': [200, screens],
    'POST /show': (body) => {
      show.screen = screens.find((s) => s.id === body.screen)!;
      show.running = true;
      return [201, state()];
    },
    'GET /show': () => (show.running ? [200, state()] : [404, { error: 'no_show' }]),
    'POST /show/next': () => {
      show.index++;
      show.playing = items[show.index].kind === 'video';
      return [200, state()];
    },
    'POST /show/player': () => [200, state()],
    'DELETE /show': () => {
      show.running = false;
      return [204];
    },
    ...extra,
  });
  return requests;
}

async function openScreens() {
  const user = userEvent.setup();
  await render(<App />);
  await user.press(await screen.findByText('Грузия'));
  await user.press(await screen.findByRole('button', { name: 'Показать на экране' }));
  return user;
}

async function startOn(name: string) {
  const user = await openScreens();
  await user.press(await screen.findByRole('button', { name }));
  await screen.findByText(`На экране «${name}»`);
  return user;
}

const presented = () => native.present.mock.calls.map(([slide]: [unknown]) => slide);

const turnOn = /включите «Повтор экрана»/;
const controlCenter = /откройте Пункт управления .*нажмите «Повтор экрана»/;
const keepOpen = 'Не блокируйте iPhone и не сворачивайте Pileup, пока идёт показ: иначе он прервётся на телевизоре.';

beforeEach(() => {
  jest.replaceProperty(Platform, 'OS', 'ios');
  native.present.mockClear();
  native.externalDisplay.mockClear();
  native.externalDisplay.mockResolvedValue(false);
});

test('on iPhone the picker tells how to turn on Screen Mirroring for an Apple TV until an external display is connected', async () => {
  core([livingRoom, bedroom]);
  await openScreens();

  const hint = await screen.findByTestId('mirroring');
  expect(within(hint).getByText(turnOn)).toBeOnTheScreen();
  expect(within(hint).getByText(controlCenter)).toBeOnTheScreen();

  await act(() => native.emit('onExternalDisplay', { connected: true }));
  expect(screen.queryByText(turnOn)).toBeNull();
  expect(screen.getByText('«Повтор экрана» включён. Выберите Apple TV, на который идёт повтор.')).toBeOnTheScreen();
});

test('the picker gives no Screen Mirroring hint without an Apple TV it can use', async () => {
  core([livingRoom]);
  await openScreens();
  await screen.findByRole('button', { name: 'Гостиная' });
  expect(screen.queryByTestId('mirroring')).toBeNull();
});

test('on Android an Apple TV is explained by its reason and the native presentation is not asked', async () => {
  jest.replaceProperty(Platform, 'OS', 'android');
  const reason = 'Показ на Apple TV доступен только с iPhone. Запустите показ с iPhone участника.';
  core([livingRoom, { ...bedroom, available: false, reason }]);
  await openScreens();
  expect(within(await screen.findByTestId(`unavailable-${bedroom.id}`)).getByText(reason)).toBeOnTheScreen();
  expect(screen.queryByTestId('mirroring')).toBeNull();
  expect(native.externalDisplay).not.toHaveBeenCalled();
});

test('a show on Apple TV hands each slide to the native presentation from the command reply and clears it on stop', async () => {
  core([bedroom], { 'GET /show': () => new Promise<Reply>(() => {}) });
  const user = await startOn('Спальня');

  expect(presented()).toEqual([{ asset: 'f3', image: t2(items[0]), around: [t2(items[1])], playing: false }]);

  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  await screen.findByText('2 / 3');
  expect(presented()[1]).toEqual({
    asset: 'a1',
    image: t2(items[1]),
    video: `${port}/a1/ra1/3`,
    around: [t2(items[2]), t2(items[0])],
    playing: true,
  });

  await user.press(screen.getByRole('button', { name: 'Остановить показ' }));
  await screen.findByText('Грузия');
  expect(presented()).toHaveLength(3);
  expect(presented()[2]).toBeNull();
});

test('a show on Cast does not touch the native presentation', async () => {
  core([livingRoom]);
  await startOn('Гостиная');
  expect(native.present).not.toHaveBeenCalled();
  expect(screen.queryByText(keepOpen)).toBeNull();
});

test('the remote tells how to turn on Screen Mirroring until the Apple TV shows the slide and asks to keep the iPhone open', async () => {
  core([bedroom]);
  await startOn('Спальня');

  const hint = await screen.findByTestId('mirroring');
  expect(within(hint).getByText(/На экране «Спальня» пока ничего нет: включите «Повтор экрана»/)).toBeOnTheScreen();
  expect(within(hint).getByText(/выберите «Спальня»/)).toBeOnTheScreen();
  expect(screen.getByText(keepOpen)).toBeOnTheScreen();

  await act(() => native.emit('onExternalDisplay', { connected: true }));
  expect(screen.queryByTestId('mirroring')).toBeNull();
  expect(screen.getByText(keepOpen)).toBeOnTheScreen();
});

test('the remote knows an external display connected before the show', async () => {
  native.externalDisplay.mockResolvedValue(true);
  core([bedroom]);
  await startOn('Спальня');
  expect(await screen.findByText(keepOpen)).toBeOnTheScreen();
  expect(screen.queryByTestId('mirroring')).toBeNull();
});

test('what the native player does with the video reaches the core', async () => {
  const requests = core([bedroom]);
  const user = await startOn('Спальня');
  await user.press(screen.getByRole('button', { name: 'Следующий' }));
  await screen.findByText('2 / 3');

  const video = 'http://192.168.1.5:50123/show/t/a1/r1/3';
  await act(() => native.emit('onPlayer', { asset: 'a1', video, state: 'buffering' }));
  await act(() => native.emit('onPlayer', { asset: 'a1', video, state: 'failed', reason: 'network', message: 'NSURLErrorDomain -1004' }));
  expect(requests.filter((r) => r.path === '/show/player').map((r) => r.body)).toEqual([
    { asset: 'a1', video, state: 'buffering' },
    { asset: 'a1', video, state: 'failed', reason: 'network', message: 'NSURLErrorDomain -1004' },
  ]);
});
