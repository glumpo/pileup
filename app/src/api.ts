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

export type Screen = {
  id: string;
  name: string;
  kind: string;
  address: string;
  available: boolean;
  reason?: string;
};

export type ShowItem = { id: string; revision: string; author: string; kind: string; taken_at: number };

export type Hint = { author: string; name: string; platform: string; reason: string };

export type ShowState = {
  pile: string;
  screen: Screen;
  index: number;
  total: number;
  current: ShowItem;
  playing: boolean;
  video_unavailable: boolean;
  renderer_error?: { code: string; message: string };
  waiting: ShowItem[];
  hints: Hint[];
};

export type PinProgress = {
  author: string;
  name: string;
  platform: string;
  total: number;
  done: number;
  failed: number;
  reason?: string;
};

export type PinEstimate = {
  files: number;
  missing: number;
  estimated_bytes: number;
  budget_bytes: number;
  free_bytes: number | null;
  over_budget: boolean;
};

export type ImageSource = { uri: string; headers: Record<string, string> };

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly reason?: string,
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
      throw new ApiError(res.status, problem.error ?? 'internal', problem.reason);
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
    reconnect: () => call<void>('POST', '/reconnect'),
    diagExport: () => text('/diag/export'),
    screens: () => call<Screen[]>('GET', '/screens'),
    startShow: (pile: string, screen: string, asset?: string) =>
      call<ShowState>('POST', '/show', { pile, screen, asset }),
    show: () => call<ShowState>('GET', '/show'),
    showCommand: (command: 'next' | 'prev' | 'play' | 'pause') => call<ShowState>('POST', `/show/${command}`),
    stopShow: () => call<void>('DELETE', '/show'),
    pins: (pile: string) => call<PinProgress[]>('GET', `/piles/${pile}/pins`),
    pinEstimate: (pile: string, member?: string) =>
      call<PinEstimate>('GET', `/piles/${pile}/pins/estimate${member ? `?member=${member}` : ''}`),
    pin: (pile: string, member?: string) => call<PinProgress[]>('POST', `/piles/${pile}/pins`, member ? { member } : {}),
    unpin: (pile: string, member?: string) =>
      call<PinProgress[]>('DELETE', `/piles/${pile}/pins${member ? `?member=${member}` : ''}`),
    blob: (pile: string, item: { id: string; revision: string }, tier: 1 | 2): ImageSource => ({
      uri: `${base}/piles/${pile}/blob/${item.id}/${item.revision}/${tier}`,
      headers: auth,
    }),
  };
}

export type Api = ReturnType<typeof connect>;
