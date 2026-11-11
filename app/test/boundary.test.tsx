import { render, screen, userEvent } from '@testing-library/react-native';

import App from '../App';
import { fakeCore } from './fakeCore';

jest.mock('../src/screens/Log', () => ({
  Log: () => {
    throw new Error('render failed');
  },
}));

test('a screen that fails to render shows a message and returns to the pile list', async () => {
  jest.spyOn(console, 'error').mockImplementation(() => {});
  jest.spyOn(console, 'warn').mockImplementation(() => {});
  fakeCore({ 'GET /piles': [200, []] });
  const user = userEvent.setup();
  await render(<App />);

  await user.press(await screen.findByRole('button', { name: 'Журнал' }));
  expect(await screen.findByText(/Этот экран не удалось показать/)).toBeOnTheScreen();
  await user.press(screen.getByRole('button', { name: 'К списку pile' }));
  expect(await screen.findByLabelText('Название поездки')).toBeOnTheScreen();
});
