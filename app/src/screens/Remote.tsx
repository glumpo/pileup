import { useKeepAwake } from 'expo-keep-awake';
import { useEffect, useMemo, useState } from 'react';
import { ScrollView, Text, View } from 'react-native';

import { Api, Hint, Pile, ShowState, TimelineItem } from '../api';
import { PinsPanel, usePins } from '../Pins';
import { adviceFor } from '../presence';
import { Preview } from '../Preview';
import { dayOf, timeOf } from '../time';
import { Button, describe, ErrorText, Screen, styles } from '../ui';

const rendererErrors: Record<string, string> = {
  load_failed: 'Экран не смог открыть этот файл. Попробуйте следующий.',
  command_failed: 'Экран не выполнил команду. Попробуйте ещё раз.',
  playback_failed: 'Экран не смог воспроизвести видео. Попробуйте следующий файл.',
  reconnecting: 'Переподключаемся к экрану…',
};

function hintText({ name, platform, reason, last_error }: Hint): string {
  const phone = `${platform === 'ios' ? 'iPhone' : 'телефон'}: ${name}`;
  const advice = adviceFor(last_error);
  return reason === 'timeout' ? `Медленно отвечает ${phone}. ${advice}` : `Нет ответа от ${phone}. ${advice}`;
}

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
  const pins = usePins(api, pile.id);
  useKeepAwake();

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
  const ownerAway = state.hints.some((h) => h.author === current.author);
  const rendererError = state.renderer_error?.code === 'load_failed' && ownerAway ? undefined : state.renderer_error;

  if (state.total === 0) {
    return (
      <Screen title={`На экране «${state.screen.name}»`}>
        <ScrollView contentContainerStyle={styles.body}>
          <Text style={{ fontSize: 17, fontWeight: '600' }}>В pile не осталось файлов</Text>
          <ErrorText error={error} />
          <Button title="Остановить показ" onPress={stop} />
        </ScrollView>
      </Screen>
    );
  }

  return (
    <Screen title={`На экране «${state.screen.name}»`}>
      <ScrollView contentContainerStyle={styles.body}>
        <View>
          <Preview
            key={`${current.id}/${current.revision}`}
            thumbhash={item?.thumbhash ?? ''}
            source={api.blob(pile.id, current, 2)}
            style={{ aspectRatio: item ? item.width / item.height : 4 / 3 }}
            resizeMode="contain"
          />
          {state.video_unavailable && (
            <Text
              style={{
                position: 'absolute',
                left: 0,
                right: 0,
                bottom: 0,
                padding: 12,
                color: '#fff',
                backgroundColor: 'rgba(0, 0, 0, 0.6)',
              }}
            >
              Видео недоступно — на экране постер. Попросите разблокировать телефон и открыть Pileup или переходите к
              следующему файлу.
            </Text>
          )}
        </View>
        <Text style={{ fontSize: 17, fontWeight: '600' }}>
          {state.index + 1} / {state.total}
        </Text>
        {author && <Text>{author}</Text>}
        <Text style={styles.muted}>
          {dayOf(item ?? current)} {timeOf(item ?? current)}
          {current.kind === 'video' ? ' · видео' : ''}
        </Text>
        {state.hints.length > 0 && (
          <View testID="waiting" style={{ backgroundColor: '#fff3cd', borderRadius: 8, padding: 12, gap: 8 }}>
            {state.hints.map((h) => (
              <View key={h.author} style={{ gap: 8 }}>
                <Text>{hintText(h)}</Text>
                <Button
                  title="Загрузить всё с участника"
                  label={`Загрузить всё с участника: ${h.name}`}
                  onPress={() => pins.ask(h)}
                />
              </View>
            ))}
          </View>
        )}
        <PinsPanel pins={pins} />
        {rendererError && (
          <ErrorText error={rendererErrors[rendererError.code] ?? `Ошибка экрана: ${rendererError.code}`} />
        )}
        {rendererError?.code === 'reconnecting' && (
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
        <Button title="Загрузить вообще всё" onPress={() => pins.ask()} />
        <Button title="Остановить показ" onPress={stop} />
      </ScrollView>
    </Screen>
  );
}
