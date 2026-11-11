import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, ScrollView, Share, Text, useWindowDimensions, View } from 'react-native';

import { Api, Pile } from '../api';
import { PinsPanel, Source, usePins } from '../Pins';
import { presenceOf } from '../presence';
import { QR } from '../QR';
import { Button, describe, ErrorText, Screen, styles } from '../ui';

export function PileScreen({
  api,
  pile: initial,
  onBack,
  onTimeline,
  onSelection,
  onShow,
}: {
  api: Api;
  pile: Pile;
  onBack: () => void;
  onTimeline: (pile: Pile) => void;
  onSelection: (pile: Pile) => void;
  onShow: (pile: Pile) => void;
}) {
  const [pile, setPile] = useState(initial);
  const [invite, setInvite] = useState<string>();
  const [error, setError] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const [refreshing, setRefreshing] = useState(false);
  const [sources, setSources] = useState<Source[]>([]);
  const pins = usePins(api, initial.id);
  const { width } = useWindowDimensions();

  const load = useCallback(async () => {
    setFailure(undefined);
    api.timeline(initial.id).then((items) => {
      const names = new Map(items.map((i) => [i.author, i.author_name]));
      setSources([...names].map(([author, name]) => ({ author, name })));
    }, (e) => setFailure(describe(e)));
    try {
      const piles = await api.piles();
      setPile(piles.find((p) => p.id === initial.id) ?? initial);
    } catch (e) {
      setFailure(describe(e));
    }
  }, [api, initial]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = () =>
      api
        .piles()
        .then(
          (piles) => {
            setPile((p) => piles.find((x) => x.id === p.id) ?? p);
            setFailure(undefined);
          },
          (e) => setFailure(describe(e)),
        )
        .finally(() => {
          if (live) timer = setTimeout(poll, 3000);
        });
    timer = setTimeout(poll, 3000);
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [api]);

  async function showInvite() {
    setError(undefined);
    try {
      setInvite(await api.invite(initial.id));
    } catch (e) {
      setError(describe(e));
    }
  }

  async function refresh() {
    setRefreshing(true);
    await load();
    setRefreshing(false);
  }

  return (
    <Screen title={pile.name || 'Pile'} onBack={onBack}>
      <ScrollView
        contentContainerStyle={[styles.body, { alignItems: 'stretch' }]}
        refreshControl={<RefreshControl refreshing={refreshing} onRefresh={refresh} />}
      >
        <Text style={{ fontSize: 17, fontWeight: '600' }}>Участники</Text>
        {pile.members.map((m) => (
          <Text key={m.key}>{presenceOf(m)}</Text>
        ))}
        <Button title="Хронология" onPress={() => onTimeline(pile)} />
        <Button title="Мои файлы" onPress={() => onSelection(pile)} />
        <Button title="Показать на экране" onPress={() => onShow(pile)} />
        <ErrorText error={failure} />
        <ErrorText error={error} />
        <Text style={{ fontSize: 17, fontWeight: '600' }}>Загрузка на это устройство</Text>
        {sources.map((s) => (
          <View key={s.author} style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
            <Text style={{ flex: 1 }}>{s.name}</Text>
            <Button
              title="Загрузить всё с участника"
              label={`Загрузить всё с участника: ${s.name}`}
              onPress={() => pins.ask(s)}
            />
          </View>
        ))}
        <Button title="Загрузить вообще всё" onPress={() => pins.ask()} />
        <PinsPanel pins={pins} />
        <Text style={{ fontSize: 17, fontWeight: '600' }}>Пригласить</Text>
        {invite ? (
          <>
            <QR text={invite} size={width - 32} />
            <Text selectable style={styles.muted}>
              {invite}
            </Text>
            <Button title="Отправить приглашение" onPress={() => Share.share({ message: invite })} />
          </>
        ) : (
          <Button title="Показать приглашение" onPress={showInvite} />
        )}
      </ScrollView>
    </Screen>
  );
}
