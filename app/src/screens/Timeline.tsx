import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { AppState, FlatList, Pressable, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import { Api, Pile, TimelineItem } from '../api';
import { Preview } from '../Preview';
import { dayOf, duration } from '../time';
import { Boundary, describe, ErrorText, Screen, styles } from '../ui';

const COLUMNS = 4;
const HEADER = 36;
const REFRESH = 10_000;

type Row = { day: string } | { first: number; items: TimelineItem[] };

function toRows(items: TimelineItem[]): Row[] {
  const rows: Row[] = [];
  let day = '';
  items.forEach((item, i) => {
    const d = dayOf(item);
    const last = rows[rows.length - 1];
    if (d !== day) {
      day = d;
      rows.push({ day: d });
      rows.push({ first: i, items: [item] });
    } else if ('items' in last && last.items.length < COLUMNS) {
      last.items.push(item);
    } else {
      rows.push({ first: i, items: [item] });
    }
  });
  return rows;
}

function same(a: TimelineItem, b: TimelineItem): boolean {
  const keys = Object.keys(a) as (keyof TimelineItem)[];
  return keys.length === Object.keys(b).length && keys.every((k) => a[k] === b[k]);
}

function merge(prev: TimelineItem[] | undefined, next: TimelineItem[]): TimelineItem[] {
  if (!prev) return next;
  const known = new Map(prev.map((i) => [i.id, i]));
  const out = next.map((i) => {
    const old = known.get(i.id);
    return old && same(old, i) ? old : i;
  });
  return out.length === prev.length && out.every((i, n) => i === prev[n]) ? prev : out;
}

function emptyText(pile: Pile): string {
  if (!pile.name)
    return 'Хронология ещё не получена: не было связи ни с одним участником. Файлы появятся, когда кто-то из них откроет Pileup в этой сети.';
  const offline = pile.members.filter((m) => !m.online).map((m) => m.name);
  if (offline.length === 0) return 'Пока нет файлов';
  return `Пока нет файлов. Нет связи с участниками: ${offline.join(', ')} — их файлы появятся, когда они откроют Pileup в этой сети.`;
}

const Cell = memo(function Cell({
  api,
  pile,
  item,
  index,
  size,
  onPress,
}: {
  api: Api;
  pile: string;
  item: TimelineItem;
  index: number;
  size: number;
  onPress: (index: number) => void;
}) {
  const source = useMemo(() => api.blob(pile, item, 1), [api, pile, item]);
  return (
    <Pressable
      testID={`asset-${item.id}`}
      onPress={() => onPress(index)}
      style={{ width: size, height: size, padding: 1 }}
    >
      <Preview thumbhash={item.thumbhash} source={source} style={{ flex: 1 }} />
      <Text style={cell.author} numberOfLines={1}>
        {item.author_name}
      </Text>
      {item.duration_ms !== undefined && <Text style={cell.duration}>▶ {duration(item.duration_ms)}</Text>}
    </Pressable>
  );
});

export function Timeline({
  api,
  pile,
  onBack,
  onOpen,
}: {
  api: Api;
  pile: Pile;
  onBack: () => void;
  onOpen: (items: TimelineItem[], index: number) => void;
}) {
  const [items, setItems] = useState<TimelineItem[]>();
  const [info, setInfo] = useState(pile);
  const [error, setError] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const { width } = useWindowDimensions();
  const size = width / COLUMNS;

  const load = useCallback(async () => {
    try {
      const next = await api.timeline(pile.id);
      if (next.length === 0) setInfo((await api.piles()).find((p) => p.id === pile.id) ?? pile);
      setItems((prev) => merge(prev, next));
      setError(undefined);
    } catch (e) {
      setError(describe(e));
    }
  }, [api, pile]);

  useEffect(() => {
    load();
    let active = true;
    const timer = setInterval(() => active && load(), REFRESH);
    const sub = AppState.addEventListener('change', (state) => {
      active = state === 'active';
      if (active) load();
    });
    return () => {
      clearInterval(timer);
      sub.remove();
    };
  }, [load]);

  async function refresh() {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  }

  const latest = useRef({ onOpen, items });
  latest.current = { onOpen, items };
  const open = useCallback((index: number) => latest.current.onOpen(latest.current.items ?? [], index), []);
  const rows = useMemo(() => toRows(items ?? []), [items]);
  const offsets = useMemo(() => {
    const out = [0];
    for (const r of rows) out.push(out[out.length - 1] + ('day' in r ? HEADER : size));
    return out;
  }, [rows, size]);

  return (
    <Screen title={pile.name || 'Хронология'} onBack={onBack}>
      <ErrorText error={error} />
      <FlatList
        testID="timeline"
        data={rows}
        keyExtractor={(r) => ('day' in r ? r.day : r.items[0].id)}
        getItemLayout={(_, i) => ({ length: offsets[i + 1] - offsets[i], offset: offsets[i], index: i })}
        initialNumToRender={12}
        maxToRenderPerBatch={8}
        windowSize={7}
        removeClippedSubviews
        refreshing={refreshing}
        onRefresh={refresh}
        ListEmptyComponent={
          items || !error ? (
            <Text style={[styles.muted, { padding: 16 }]}>{items ? emptyText(info) : 'Загружаем хронологию…'}</Text>
          ) : undefined
        }
        renderItem={({ item: row }) =>
          'day' in row ? (
            <Text style={cell.day}>{row.day}</Text>
          ) : (
            <View style={{ flexDirection: 'row' }}>
              {row.items.map((item, i) => (
                <Boundary
                  key={item.id}
                  item={item}
                  fallback={
                    <View style={{ width: size, height: size, padding: 1 }}>
                      <Text style={cell.broken}>Не удалось показать файл</Text>
                    </View>
                  }
                >
                  <Cell api={api} pile={pile.id} item={item} index={row.first + i} size={size} onPress={open} />
                </Boundary>
              ))}
            </View>
          )
        }
      />
    </Screen>
  );
}

const cell = StyleSheet.create({
  day: { height: HEADER, paddingHorizontal: 12, paddingTop: 12, fontWeight: '600' },
  author: {
    position: 'absolute',
    left: 1,
    right: 1,
    bottom: 1,
    paddingHorizontal: 4,
    fontSize: 11,
    color: '#fff',
    backgroundColor: 'rgba(0,0,0,0.45)',
  },
  duration: { position: 'absolute', top: 3, right: 5, fontSize: 11, color: '#fff' },
  broken: { flex: 1, padding: 4, fontSize: 11, color: '#777', backgroundColor: '#ddd' },
});
