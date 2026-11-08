jest.mock('../modules/core/src/PileupCoreModule', () => ({
  __esModule: true,
  default: {
    start: jest.fn(() => Promise.resolve()),
    stop: jest.fn(() => Promise.resolve()),
    port: () => 4321,
    token: () => 'test-token',
    requestMediaPermissions: jest.fn(() => Promise.resolve(true)),
  },
}));
