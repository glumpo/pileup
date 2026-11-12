import { act, render, screen, userEvent } from '@testing-library/react-native';

import App from '../App';
import { fakeCore, georgia, INVITE } from './fakeCore';

jest.mock('expo-keep-awake', () => {
  const { useEffect } = require('react');
  const held = new Set<string>();
  const activateKeepAwakeAsync = async (tag = 'default') => {
    held.add(tag);
  };
  const deactivateKeepAwake = async (tag = 'default') => {
    held.delete(tag);
  };
  return {
    held,
    activateKeepAwakeAsync,
    deactivateKeepAwake,
    useKeepAwake: (tag = 'hook') =>
      useEffect(() => {
        activateKeepAwakeAsync(tag);
        return () => {
          deactivateKeepAwake(tag);
        };
      }, [tag]),
  };
});

const screenKeptOn = () => jest.requireMock<{ held: Set<string> }>('expo-keep-awake').held.size > 0;
const banner = 'Не блокируйте телефон и не сворачивайте Pileup: с него идёт видео на экран.';
const wait = (ms: number) => act(() => jest.advanceTimersByTimeAsync(ms));

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

test('while this phone serves a video the screen is kept on and a banner is shown on any screen, with a margin after', async () => {
  let serving = false;
  fakeCore({
    'GET /piles': [200, [georgia]],
    'GET /piles/aa01/invite': [200, { invite: INVITE }],
    'GET /piles/aa01/timeline': [200, []],
    'GET /piles/aa01/pins': [200, []],
    'GET /serving': () => [200, { serving }],
  });
  const user = userEvent.setup({ advanceTimers: jest.advanceTimersByTime });
  await render(<App />);
  await screen.findByText('Грузия');
  await wait(5000);
  expect(screen.queryByText(banner)).toBeNull();
  expect(screenKeptOn()).toBe(false);

  serving = true;
  await wait(5000);
  expect(screen.getByText(banner)).toBeOnTheScreen();
  expect(screenKeptOn()).toBe(true);

  await user.press(screen.getByText('Грузия'));
  await screen.findByRole('button', { name: 'Хронология' });
  expect(screen.getByText(banner)).toBeOnTheScreen();

  serving = false;
  await wait(45_000);
  expect(screen.getByText(banner)).toBeOnTheScreen();
  expect(screenKeptOn()).toBe(true);

  await wait(30_000);
  expect(screen.queryByText(banner)).toBeNull();
  expect(screenKeptOn()).toBe(false);
});
