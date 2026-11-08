import { useEffect, useState } from 'react';
import { FlatList, Pressable, Text, View } from 'react-native';

import { Api, Pile } from '../api';
import { Button, describe, ErrorText, Field, Screen, styles } from '../ui';

export function Piles({ api, onOpen, onJoin }: { api: Api; onOpen: (pile: Pile) => void; onJoin: () => void }) {
  const [piles, setPiles] = useState<Pile[]>([]);
  const [name, setName] = useState('');
  const [member, setMember] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  useEffect(() => {
    api.piles().then(setPiles, (e) => setError(describe(e)));
  }, [api]);

  async function create() {
    setBusy(true);
    setError(undefined);
    try {
      onOpen(await api.createPile(name.trim(), member.trim()));
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen title="Pileup">
      <FlatList
        data={piles}
        keyExtractor={(p) => p.id}
        renderItem={({ item }) => (
          <Pressable accessibilityRole="button" style={styles.row} onPress={() => onOpen(item)}>
            <Text style={{ fontSize: 17 }}>{item.name || 'Без названия'}</Text>
            <Text style={styles.muted}>{item.members.map((m) => m.name).join(', ')}</Text>
          </Pressable>
        )}
        ListFooterComponent={
          <View style={styles.body}>
            <Text style={{ fontSize: 17, fontWeight: '600' }}>Новый pile</Text>
            <Field label="Название поездки" value={name} onChangeText={setName} />
            <Field label="Ваше имя" value={member} onChangeText={setMember} />
            <Button title="Создать" onPress={create} disabled={busy || !name.trim() || !member.trim()} />
            <ErrorText error={error} />
            <Button title="Войти по приглашению" onPress={onJoin} />
          </View>
        }
      />
    </Screen>
  );
}
