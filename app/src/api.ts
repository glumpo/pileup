export type Member = { name: string; platform: string };

export type Pile = { id: string; name: string; members: Member[] };

export type SelectionAsset = {
  id: string;
  local_id: string;
  kind: string;
  taken_at: number;
  original_name: string;
  screenshot: boolean;
  hidden: boolean;
  selected: boolean;
};

export type Selection = { from: number; to: number; assets: SelectionAsset[] };

export type TimelineItem = {
  id: string;
  revision: string;
  author: string;
  author_name: string;
  kind: string;
  taken_at: number;
  local_offset?: number;
  duration_ms?: number;
  width: number;
  height: number;
  thumbhash: string;
};

export type DiagEvent = {
  time: number;
  kind: string;
  pile?: string;
  peer?: string;
  message: string;
  count: number;
};

export type ImageSource = { uri: string; headers: Record<string, string> };

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(code);
  }
}

export function connect(port: number, token: string) {
  const base = `http://127.0.0.1:${port}`;
  const auth = { Authorization: `Bearer ${token}` };

  async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(base + path, {
      method,
      headers: body === undefined ? auth : { ...auth, 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      const problem = await res.json().catch(() => ({}));
      throw new ApiError(res.status, problem.error ?? 'internal');
    }
    return res.status === 204 ? (undefined as T) : res.json();
  }

  async function text(path: string): Promise<string> {
    const res = await fetch(base + path, { headers: auth });
    if (!res.ok) throw new ApiError(res.status, 'internal');
    return res.text();
  }

  return {
    piles: () => call<Pile[]>('GET', '/piles'),
    createPile: (name: string, member: string) => call<Pile>('POST', '/piles', { name, member }),
    invite: async (pile: string) => (await call<{ invite: string }>('GET', `/piles/${pile}/invite`)).invite,
    join: (invite: string, member: string) => call<Pile>('POST', '/piles/join', { invite, member }),
    selection: (pile: string) => call<Selection>('GET', `/piles/${pile}/selection`),
    setRange: (pile: string, from: number, to: number) =>
      call<void>('PUT', `/piles/${pile}/selection`, { from, to }),
    setSelected: (pile: string, asset: string, selected: boolean) =>
      call<void>('PUT', `/piles/${pile}/selection/${asset}`, { selected }),
    timeline: (pile: string) => call<TimelineItem[]>('GET', `/piles/${pile}/timeline`),
    diag: () => call<DiagEvent[]>('GET', '/diag'),
    diagExport: () => text('/diag/export'),
    blob: (pile: string, item: TimelineItem, tier: 1 | 2): ImageSource => ({
      uri: `${base}/piles/${pile}/blob/${item.id}/${item.revision}/${tier}`,
      headers: auth,
    }),
  };
}

export type Api = ReturnType<typeof connect>;
