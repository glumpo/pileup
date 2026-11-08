import { useEffect, useMemo, useState } from 'react';
import { ScrollView, Text, View } from 'react-native';

import { Api, Pile, ShowState, TimelineItem } from '../api';
import { Preview } from '../Preview';
import { dayOf, timeOf } from '../time';
import { Button, describe, ErrorText, Screen, styles } from '../ui';

const rendererErrors: Record<string, string> = {
  load_failed: 'Экран не смог открыть этот файл. Попробуйте следующий.',
  command_failed: 'Экран не выполнил команду. Попробуйте ещё раз.',
  playback_failed: 'Экран не смог воспроизвести видео. Попробуйте следующий файл.',
  disconnected: 'Связь с экраном потеряна.',
};

export function Remote({
  api,
  pile,
  initial,
  onStop,
}: {
  api: Api;
  pile: Pile;
  initial: ShowState;
  onStop: () => void;
}) {
  const [state, setState] = useState(initial);
  const [timeline, setTimeline] = useState<TimelineItem[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => {
    const fail = (e: unknown) => setError(describe(e));
    api.timeline(pile.id).then(setTimeline, fail);
    const timer = setInterval(() => api.show().then(setState, fail), 1000);
    return () => clearInterval(timer);
  }, [api, pile.id]);

  const byId = useMemo(() => new Map(timeline.map((i) => [i.id, i])), [timeline]);
  const names = useMemo(() => new Map(timeline.map((i) => [i.author, i.author_name])), [timeline]);

  async function run(command: () => Promise<ShowState>) {
    setError(undefined);
    try {
      setState(await command());
    } catch (e) {
      setError(describe(e));
    }
  }

  async function stop() {
    try {
      await api.stopShow();
      onStop();
    } catch (e) {
      setError(describe(e));
    }
  }

  const { current } = state;
  const item = byId.get(current.id);
  const author = names.get(current.author);
  const waiting = [...new Set(state.waiting.map((w) => w.author))].map((a) => {
    const name = names.get(a);
    const member = pile.members.find((m) => m.name === name);
    return `Ждём ${member?.platform === 'ios' ? 'iPhone' : 'телефон'}${name ? `: ${name}` : ''}`;
  });
  const rendererError = state.renderer_error;

  return (
    <Screen title={`На экране «${state.screen.name}»`}>
      <ScrollView contentContainerStyle={styles.body}>
        <Preview
          key={`${current.id}/${current.revision}`}
          thumbhash={item?.thumbhash ?? ''}
          source={api.blob(pile.id, current, 2)}
          style={{ aspectRatio: item ? item.width / item.height : 4 / 3 }}
          resizeMode="contain"
        />
        <Text style={{ fontSize: 17, fontWeight: '600' }}>
          {state.index + 1} / {state.total}
        </Text>
        {author && <Text>{author}</Text>}
        <Text style={styles.muted}>
          {dayOf(item ?? current)} {timeOf(item ?? current)}
          {current.kind === 'video' ? ' · видео' : ''}
        </Text>
        {waiting.length > 0 && (
          <View testID="waiting" style={{ backgroundColor: '#fff3cd', borderRadius: 8, padding: 12, gap: 4 }}>
            {waiting.map((w) => (
              <Text key={w}>{w}</Text>
            ))}
            <Text style={styles.muted}>Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.</Text>
          </View>
        )}
        {rendererError && (
          <ErrorText error={rendererErrors[rendererError.code] ?? `Ошибка экрана: ${rendererError.code}`} />
        )}
        {rendererError?.code === 'disconnected' && (
          <Button
            title="Перезапустить показ"
            onPress={() => run(() => api.startShow(pile.id, state.screen.id, current.id))}
          />
        )}
        <ErrorText error={error} />
        <View style={{ flexDirection: 'row', gap: 12 }}>
          <View style={{ flex: 1 }}>
            <Button title="Предыдущий" onPress={() => run(() => api.showCommand('prev'))} />
          </View>
          <View style={{ flex: 1 }}>
            <Button title="Следующий" onPress={() => run(() => api.showCommand('next'))} />
          </View>
        </View>
        {current.kind === 'video' && (
          <Button
            title={state.playing ? 'Пауза' : 'Воспроизвести'}
            onPress={() => run(() => api.showCommand(state.playing ? 'pause' : 'play'))}
          />
        )}
        <Button title="Остановить показ" onPress={stop} />
      </ScrollView>
    </Screen>
  );
}
