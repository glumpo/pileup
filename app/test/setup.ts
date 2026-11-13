jest.mock('../modules/core/src/PileupCoreModule', () => {
  const listeners = new Map<string, Set<(event: unknown) => void>>();
  return {
    __esModule: true,
    default: {
      start: jest.fn(() => Promise.resolve()),
      stop: jest.fn(() => Promise.resolve()),
      port: () => 4321,
      token: () => 'test-token',
      requestMediaPermissions: jest.fn(() => Promise.resolve(true)),
      present: jest.fn(() => Promise.resolve()),
      externalDisplay: jest.fn(() => Promise.resolve(false)),
      addListener: jest.fn((name: string, listener: (event: unknown) => void) => {
        listeners.set(name, (listeners.get(name) ?? new Set()).add(listener));
        return { remove: () => listeners.get(name)?.delete(listener) };
      }),
      emit: (name: string, event: unknown) => listeners.get(name)?.forEach((listener) => listener(event)),
    },
  };
});

jest.mock('react-native-safe-area-context', () => require('react-native-safe-area-context/jest/mock').default);
