import { useCallback, useEffect, useState } from 'react';
import { FlatList, Linking, Platform, Switch, Text, View } from 'react-native';

import { Api, ApiError, Pile, Selection } from '../api';
import { requestMediaPermissions } from '../core';
import { dayOf, timeOf } from '../time';
import { Button, describe, ErrorText, Field, Screen, styles } from '../ui';

function parseDay(s: string, after = 0): number | undefined {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s.trim());
  return m ? new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]) + after).getTime() : undefined;
}

export function SelectionScreen({ api, pile, onBack }: { api: Api; pile: Pile; onBack: () => void }) {
  const [allowed, setAllowed] = useState<boolean>();
  const [selection, setSelection] = useState<Selection>();
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  const load = useCallback(async () => {
    const s = await api.selection(pile.id);
    setSelection(s);
    if (s.to > s.from) {
      setFrom(dayOf({ taken_at: s.from }));
      setTo(dayOf({ taken_at: s.to - 1 }));
    }
  }, [api, pile.id]);

  const start = useCallback(async () => {
    setError(undefined);
    try {
      const ok = await requestMediaPermissions();
      setAllowed(ok);
      if (ok) await load();
    } catch (e) {
      setError(describe(e));
    }
  }, [load]);

  useEffect(() => {
    start();
  }, [start]);

  const first = parseDay(from);
  const end = parseDay(to, 1);

  async function apply() {
    if (first === undefined || end === undefined) return;
    setBusy(true);
    setError(undefined);
    try {
      await api.setRange(pile.id, first, end);
      await load();
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  async function toggle(asset: string, selected: boolean) {
    setError(undefined);
    try {
      await api.setSelected(pile.id, asset, selected);
      setSelection((s) => s && { ...s, assets: s.assets.map((a) => (a.id === asset ? { ...a, selected } : a)) });
    } catch (e) {
      const gone = e instanceof ApiError && e.code === 'unknown_asset';
      setError(gone ? 'Файла больше нет в галерее — список обновлён.' : describe(e));
      if (gone) load().catch((e) => setError(describe(e)));
    }
  }

  if (allowed === false) {
    return (
      <Screen title="Мои файлы" onBack={onBack}>
        <View style={styles.body}>
          <Text>Нет доступа к медиатеке. Разрешите доступ к фото и видео, чтобы добавить их в pile.</Text>
          <Button title="Разрешить" onPress={start} />
          <Text style={styles.muted}>
            {Platform.OS === 'ios'
              ? 'Если окно с вопросом больше не появляется, откройте настройки Pileup, пункт «Фото», и выберите «Полный доступ».'
              : 'Если окно с вопросом больше не появляется, откройте настройки Pileup, раздел «Разрешения», и разрешите доступ к фото и видео.'}
          </Text>
          <Button title="Открыть настройки" onPress={() => Linking.openSettings()} />
          <ErrorText error={error} />
        </View>
      </Screen>
    );
  }

  return (
    <Screen title="Мои файлы" onBack={onBack}>
      <FlatList
        data={selection?.assets ?? []}
        keyExtractor={(a) => a.id}
        ListHeaderComponent={
          <View style={styles.body}>
            <Text style={styles.muted}>Другие участники увидят только выбранные файлы.</Text>
            <Field label="С (ГГГГ-ММ-ДД)" value={from} onChangeText={setFrom} />
            <Field label="По (ГГГГ-ММ-ДД)" value={to} onChangeText={setTo} />
            <Button title="Выбрать период" onPress={apply} disabled={busy || first === undefined || end === undefined || end <= first} />
            <ErrorText error={error} />
          </View>
        }
        renderItem={({ item }) => (
          <View style={styles.row}>
            <View style={{ flex: 1 }}>
              <Text>{item.original_name}</Text>
              <Text style={styles.muted}>
                {dayOf(item)} {timeOf(item)}
                {item.screenshot ? ' · скриншот' : ''}
                {item.hidden ? ' · скрытый альбом' : ''}
              </Text>
            </View>
            <Switch
              accessibilityLabel={`В pile: ${item.original_name}`}
              value={item.selected}
              onValueChange={(v) => toggle(item.id, v)}
            />
          </View>
        )}
      />
    </Screen>
  );
}
