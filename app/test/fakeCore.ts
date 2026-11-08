import { Pile, TimelineItem } from '../src/api';

export const BASE = 'http://127.0.0.1:4321';

export const AUTH = { Authorization: 'Bearer test-token' };

type Reply = [number, unknown?];

export type Request = { method: string; path: string; body: unknown; headers: Record<string, string> };

export function fakeCore(routes: Record<string, Reply | ((body: any) => Reply)>): Request[] {
  const requests: Request[] = [];
  globalThis.fetch = jest.fn(async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const path = url.slice(BASE.length);
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined;
    requests.push({ method, path, body, headers: init.headers as Record<string, string> });
    const route = routes[`${method} ${path}`];
    const [status, reply] =
      route === undefined ? [404, { error: 'unknown_pile' }] : typeof route === 'function' ? route(body) : route;
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => reply,
      text: async () => (typeof reply === 'string' ? reply : JSON.stringify(reply)),
    } as Response;
  }) as unknown as typeof fetch;
  return requests;
}

export const INVITE = 'PILEUP:AE' + 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.repeat(4);

export const georgia: Pile = {
  id: 'aa01',
  name: 'Грузия',
  members: [
    { name: 'Аня', platform: 'ios' },
    { name: 'Боря', platform: 'android' },
  ],
};

export function asset(id: string, author: string, takenAt: number, extra: Partial<TimelineItem> = {}): TimelineItem {
  return {
    id,
    revision: `r${id}`,
    author: author === 'Аня' ? '01' : '02',
    author_name: author,
    kind: 'photo',
    taken_at: takenAt,
    width: 4000,
    height: 3000,
    thumbhash: '3wcCnxppii9zh4hziEiHl4d/cgiHh4AH',
    ...extra,
  };
}
