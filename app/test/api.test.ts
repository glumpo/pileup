import { connect } from '../src/api';
import { asset, fakeCore } from './fakeCore';

test('an unchanged timeline is asked for by its tag and is not downloaded again', async () => {
  const items = [asset('a1', 'Аня', 1), asset('b1', 'Боря', 2)];
  let asked = 0;
  const requests = fakeCore({
    'GET /piles/aa01/timeline': () => (asked++ === 0 ? [200, items, { ETag: '"v1"' }] : [304]),
  });
  const api = connect(4321, 'test-token');

  const first = await api.timeline('aa01');
  const second = await api.timeline('aa01');

  expect(first).toEqual(items);
  expect(second).toBe(first);
  expect(requests[0].headers['If-None-Match']).toBeUndefined();
  expect(requests[1].headers['If-None-Match']).toBe('"v1"');
});

test('a changed timeline replaces the remembered one', async () => {
  const before = [asset('a1', 'Аня', 1)];
  const after = [asset('a1', 'Аня', 1), asset('b1', 'Боря', 2)];
  const replies = [
    [200, before, { ETag: '"v1"' }],
    [200, after, { ETag: '"v2"' }],
    [304],
  ] as const;
  let asked = 0;
  const requests = fakeCore({ 'GET /piles/aa01/timeline': () => [...replies[asked++]] as [number, unknown?] });
  const api = connect(4321, 'test-token');

  await api.timeline('aa01');
  expect(await api.timeline('aa01')).toEqual(after);
  expect(await api.timeline('aa01')).toEqual(after);
  expect(requests[2].headers['If-None-Match']).toBe('"v2"');
});

test('timelines of different piles are remembered apart', async () => {
  const requests = fakeCore({
    'GET /piles/aa01/timeline': [200, [asset('a1', 'Аня', 1)], { ETag: '"one"' }],
    'GET /piles/bb02/timeline': [200, [], { ETag: '"two"' }],
  });
  const api = connect(4321, 'test-token');

  await api.timeline('aa01');
  await api.timeline('bb02');
  await api.timeline('aa01');

  expect(requests[1].headers['If-None-Match']).toBeUndefined();
  expect(requests[2].headers['If-None-Match']).toBe('"one"');
});
