import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, Text, View } from 'react-native';

import { Api, Pile, Screen as Display, ShowState } from '../api';
import { describe, ErrorText, Screen, styles } from '../ui';
import { LocalNetworkBanner } from '../LocalNetwork';

const kinds: Record<string, string> = { cast: 'Google Cast', airplay: 'AirPlay' };

export function ScreenPicker({
  api,
  pile,
  asset,
  onBack,
  onStarted,
}: {
  api: Api;
  pile: Pile;
  asset?: string;
  onBack: () => void;
  onStarted: (state: ShowState) => void;
}) {
  const [screens, setScreens] = useState<Display[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [starting, setStarting] = useState<string>();
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    setRefreshing(true);
    try {
      setScreens(await api.screens());
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

  async function start(screen: Display) {
    setStarting(screen.id);
    setError(undefined);
    try {
      onStarted(await api.startShow(pile.id, screen.id, asset));
    } catch (e) {
      setError(describe(e));
      setStarting(undefined);
    }
  }

  return (
    <Screen title="Показать на экране" onBack={onBack}>
      <LocalNetworkBanner api={api} />
      <ErrorText error={error} />
      <FlatList
        testID="screens"
        data={screens}
        keyExtractor={(s) => s.id}
        refreshing={refreshing}
        onRefresh={load}
        ListEmptyComponent={
          refreshing ? undefined : (
            <Text style={[styles.muted, { padding: 16 }]}>
              Экраны не найдены. Проверьте, что телевизор включён и подключён к той же сети Wi-Fi, и потяните список
              вниз, чтобы обновить.
            </Text>
          )
        }
        renderItem={({ item }) =>
          item.available ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={item.name}
              accessibilityState={{ disabled: starting !== undefined }}
              disabled={starting !== undefined}
              style={styles.row}
              onPress={() => start(item)}
            >
              <View style={{ flex: 1 }}>
                <Text style={{ fontSize: 17 }}>{item.name}</Text>
                <Text style={styles.muted}>
                  {starting === item.id ? 'Подключение к экрану…' : (kinds[item.kind] ?? item.kind)}
                </Text>
              </View>
            </Pressable>
          ) : (
            <View testID={`unavailable-${item.id}`} style={styles.row}>
              <View style={{ flex: 1 }}>
                <Text style={{ fontSize: 17 }}>{item.name}</Text>
                <Text>{item.reason}</Text>
              </View>
            </View>
          )
        }
      />
    </Screen>
  );
}
