import { useCallback, useEffect, useState } from 'react';
import { RefreshControl, ScrollView, Share, Text, useWindowDimensions } from 'react-native';

import { Api, Pile } from '../api';
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
  const [refreshing, setRefreshing] = useState(false);
  const { width } = useWindowDimensions();

  const load = useCallback(async () => {
    setError(undefined);
    try {
      const [piles, inv] = await Promise.all([api.piles(), api.invite(initial.id)]);
      setPile(piles.find((p) => p.id === initial.id) ?? initial);
      setInvite(inv);
    } catch (e) {
      setError(describe(e));
    }
  }, [api, initial]);

  useEffect(() => {
    load();
  }, [load]);

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
        <Text style={styles.muted}>Участники: {pile.members.map((m) => m.name).join(', ')}</Text>
        <Button title="Хронология" onPress={() => onTimeline(pile)} />
        <Button title="Мои файлы" onPress={() => onSelection(pile)} />
        <Button title="Показать на экране" onPress={() => onShow(pile)} />
        <ErrorText error={error} />
        {invite && (
          <>
            <Text style={{ fontSize: 17, fontWeight: '600' }}>Пригласить</Text>
            <QR text={invite} size={width - 32} />
            <Text selectable style={styles.muted}>
              {invite}
            </Text>
            <Button title="Отправить приглашение" onPress={() => Share.share({ message: invite })} />
          </>
        )}
      </ScrollView>
    </Screen>
  );
}
