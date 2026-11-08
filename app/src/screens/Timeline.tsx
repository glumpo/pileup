import { memo, useCallback, useEffect, useMemo, useState } from 'react';
import { FlatList, Pressable, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import { Api, Pile, TimelineItem } from '../api';
import { Preview } from '../Preview';
import { dayOf, duration } from '../time';
import { describe, ErrorText, Screen, styles } from '../ui';

const COLUMNS = 4;
const HEADER = 36;

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
  const [items, setItems] = useState<TimelineItem[]>([]);
  const [error, setError] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const { width } = useWindowDimensions();
  const size = width / COLUMNS;

  const load = useCallback(async () => {
    setError(undefined);
    try {
      setItems(await api.timeline(pile.id));
    } catch (e) {
      setError(describe(e));
    }
  }, [api, pile.id]);

  useEffect(() => {
    load();
  }, [load]);

  async function refresh() {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  }

  const open = useCallback((index: number) => onOpen(items, index), [onOpen, items]);
  const rows = useMemo(() => toRows(items), [items]);
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
        ListEmptyComponent={<Text style={[styles.muted, { padding: 16 }]}>Пока нет файлов</Text>}
        renderItem={({ item: row }) =>
          'day' in row ? (
            <Text style={cell.day}>{row.day}</Text>
          ) : (
            <View style={{ flexDirection: 'row' }}>
              {row.items.map((item, i) => (
                <Cell
                  key={item.id}
                  api={api}
                  pile={pile.id}
                  item={item}
                  index={row.first + i}
                  size={size}
                  onPress={open}
                />
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
});
