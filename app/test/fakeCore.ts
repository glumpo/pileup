import { Pile, TimelineItem } from '../src/api';

export const BASE = 'http://127.0.0.1:4321';

export const AUTH = { Authorization: 'Bearer test-token' };

export type Reply = [number, unknown?, Record<string, string>?];

export type Request = { url: string; method: string; path: string; body: unknown; headers: Record<string, string> };

export function fakeCore(routes: Record<string, Reply | ((body: any) => Reply | Promise<Reply>)>): Request[] {
  const requests: Request[] = [];
  globalThis.fetch = jest.fn(async (url: string, init: RequestInit = {}) => {
    const method = init.method ?? 'GET';
    const path = url.slice(BASE.length);
    const body = typeof init.body === 'string' ? JSON.parse(init.body) : undefined;
    requests.push({ url, method, path, body, headers: init.headers as Record<string, string> });
    const route = routes[`${method} ${path}`];
    const [status, reply, headers]: Reply =
      route === undefined ? [404, { error: 'unknown_pile' }] : typeof route === 'function' ? await route(body) : route;
    return {
      ok: status >= 200 && status < 300,
      status,
      headers: { get: (name: string) => headers?.[name] ?? null },
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
    { key: '01', name: 'Аня', platform: 'ios', online: true, last_seen: Date.UTC(2026, 6, 1, 6, 0) },
    { key: '02', name: 'Боря', platform: 'android', online: true, last_seen: Date.UTC(2026, 6, 1, 6, 0) },
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

export const sourceOf = (image: { props: { source?: unknown } }) =>
  [image.props.source].flat()[0] as { uri: string; headers?: Record<string, string> };
