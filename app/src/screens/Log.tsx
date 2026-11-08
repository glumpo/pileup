import { useCallback, useEffect, useState } from 'react';
import { FlatList, Share, Text, View } from 'react-native';

import { Api, DiagEvent } from '../api';
import { Button, describe, ErrorText, Screen, styles } from '../ui';

const kinds: Record<string, string> = {
  discovery: 'Обнаружение',
  connection: 'Подключение',
  sync: 'Синхронизация',
  timeout: 'Таймаут',
  renderer: 'Экран',
  media: 'Файлы',
};

const two = (n: number) => String(n).padStart(2, '0');

function stamp(ms: number): string {
  const d = new Date(ms);
  return `${two(d.getDate())}.${two(d.getMonth() + 1)} ${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}`;
}

export function Log({ api, onBack }: { api: Api; onBack: () => void }) {
  const [events, setEvents] = useState<DiagEvent[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      setEvents(await api.diag());
      setError(undefined);
    } catch (e) {
      setError(describe(e));
    } finally {
      setRefreshing(false);
    }
  }, [api]);

  useEffect(() => {
    load();
  }, [load]);

  async function share() {
    try {
      await Share.share({ title: 'Журнал Pileup', message: await api.diagExport() });
    } catch (e) {
      setError(describe(e));
    }
  }

  return (
    <Screen title="Журнал" onBack={onBack}>
      <View style={styles.body}>
        <Button title="Экспортировать" onPress={share} />
        <ErrorText error={error} />
      </View>
      <FlatList
        data={events}
        keyExtractor={(e, i) => `${i}-${e.time}`}
        refreshing={refreshing}
        onRefresh={load}
        ListEmptyComponent={<Text style={[styles.muted, { padding: 16 }]}>Событий нет</Text>}
        renderItem={({ item }) => (
          <View style={{ paddingVertical: 8, paddingHorizontal: 16, gap: 2 }}>
            <Text style={styles.muted}>
              {stamp(item.time)} · {kinds[item.kind] ?? item.kind}
              {item.count > 1 ? ` · ×${item.count}` : ''}
              {item.peer ? ` · ${item.peer}` : ''}
            </Text>
            <Text>{item.message}</Text>
          </View>
        )}
      />
    </Screen>
  );
}
