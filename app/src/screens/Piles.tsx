import { useEffect, useState } from 'react';
import { FlatList, Pressable, Text, View } from 'react-native';

import { Api, InterruptedShow, Pile, ShowState } from '../api';
import { Button, describe, ErrorText, Field, Screen, styles } from '../ui';

const brokenText: Record<string, string> = {
  corrupt: 'Не открылся: данные pile на этом телефоне повреждены. Остальные pile работают, подробности — в журнале',
  no_space: 'Не открылся: на телефоне закончилось место. Освободите место и нажмите «Повторить»',
  failed: 'Не открылся из-за ошибки. Подробности — в журнале; можно нажать «Повторить»',
};

export function Piles({
  api,
  onOpen,
  onJoin,
  onLog,
  onRestart,
  onShow,
  onPick,
}: {
  api: Api;
  onOpen: (pile: Pile) => void;
  onJoin: () => void;
  onLog: () => void;
  onRestart: () => void;
  onShow: (pile: Pile, state: ShowState) => void;
  onPick: (pile: Pile, asset: string) => void;
}) {
  const [piles, setPiles] = useState<Pile[]>([]);
  const [name, setName] = useState('');
  const [member, setMember] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [interrupted, setInterrupted] = useState<InterruptedShow>();
  const [resuming, setResuming] = useState(false);
  const [resumeError, setResumeError] = useState<string>();

  useEffect(() => {
    api.piles().then((p) => {
      setPiles(p);
      setError(undefined);
    }, (e) => setError(describe(e)));
    api.interruptedShow().then(setInterrupted, () => setInterrupted(undefined));
  }, [api]);

  const paused = interrupted && piles.find((p) => p.id === interrupted.pile && !p.broken);

  async function resume(pile: Pile, show: InterruptedShow) {
    setResuming(true);
    setResumeError(undefined);
    try {
      onShow(pile, await api.startShow(pile.id, show.screen.id, show.asset || undefined));
    } catch (e) {
      setResumeError(describe(e));
      setResuming(false);
    }
  }

  function forget() {
    setInterrupted(undefined);
    api.forgetShow().catch(() => {});
  }

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
        automaticallyAdjustKeyboardInsets
        keyExtractor={(p) => p.id}
        ListHeaderComponent={
          interrupted && paused ? (
            <View style={[styles.body, { backgroundColor: '#fff3cd', borderRadius: 8, margin: 16, gap: 8 }]}>
              <Text style={{ fontSize: 17 }}>Показ на «{interrupted.screen.name}» прервался.</Text>
              <ErrorText error={resumeError} />
              <Button title="Продолжить" onPress={() => resume(paused, interrupted)} disabled={resuming} />
              {resumeError && <Button title="Выбрать экран" onPress={() => onPick(paused, interrupted.asset)} />}
              <Button title="Забыть" onPress={forget} disabled={resuming} />
            </View>
          ) : undefined
        }
        renderItem={({ item }) =>
          item.broken ? (
            <View style={styles.body}>
              <Text style={{ fontSize: 17 }}>{item.name || 'Без названия'}</Text>
              <Text style={styles.error}>{brokenText[item.broken] ?? brokenText.failed}</Text>
              {item.broken !== 'corrupt' && <Button title="Повторить" onPress={onRestart} />}
            </View>
          ) : (
            <Pressable accessibilityRole="button" style={styles.row} onPress={() => onOpen(item)}>
              <Text style={{ fontSize: 17 }}>{item.name || 'Без названия'}</Text>
              <Text style={styles.muted}>{item.members.map((m) => m.name).join(', ')}</Text>
            </Pressable>
          )
        }
        ListFooterComponent={
          <View style={styles.body}>
            <Text style={{ fontSize: 17, fontWeight: '600' }}>Новый pile</Text>
            <Field label="Название поездки" value={name} onChangeText={setName} />
            <Field label="Ваше имя" value={member} onChangeText={setMember} />
            <Button title="Создать" onPress={create} disabled={busy || !name.trim() || !member.trim()} />
            <ErrorText error={error} />
            <Button title="Войти по приглашению" onPress={onJoin} />
            <Button title="Журнал" onPress={onLog} />
          </View>
        }
      />
    </Screen>
  );
}
