import { useKeepAwake } from 'expo-keep-awake';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ScrollView, Text, View } from 'react-native';

import { Api, ApiError, Hint, Pile, ShowState, TimelineItem } from '../api';
import { diskFull, PinsPanel, usePins } from '../Pins';
import { adviceFor } from '../presence';
import { Preview } from '../Preview';
import { dayOf, timeOf } from '../time';
import { Button, describe, ErrorText, Screen, styles } from '../ui';

const rendererErrors: Record<string, string> = {
  load_failed: 'Экран не смог открыть этот файл. Попробуйте следующий.',
  command_failed: 'Экран не выполнил команду. Попробуйте ещё раз.',
  reconnecting: 'Переподключаемся к экрану…',
};

const formats: Record<string, string> = { hvc1: 'HEVC', hev1: 'HEVC', avc1: 'H.264', avc3: 'H.264' };

function videoText(reason: string | undefined, codec: string | undefined, author: string): string {
  if (reason === 'unsupported') {
    const format = codec ? ` в формате ${formats[codec] ?? codec}` : ' — вероятно, не поддерживает его формат';
    return `Экран не воспроизводит это видео${format}. На экране постер. Посмотрите видео на телефоне, где оно снято, или переходите к следующему файлу.`;
  }
  if (reason === 'not_local')
    return `Видео участника: ${author} — только в iCloud, на телефоне его нет. Откройте его в «Фото» на iPhone, чтобы скачать, и нажмите «Воспроизвести» или переходите к следующему файлу.`;
  if (reason === 'unreadable')
    return `Телефон участника не может прочитать это видео: ${author}. Возможно, оно удалено из галереи или у Pileup нет к нему доступа. Переходите к следующему файлу.`;
  return 'Видео недоступно — на экране постер. Попросите разблокировать телефон и открыть Pileup или переходите к следующему файлу.';
}

function hintText({ name, platform, reason, last_error }: Hint): string {
  const advice = adviceFor(last_error);
  return reason === 'timeout'
    ? `Медленно отвечает ${platform === 'ios' ? 'iPhone' : 'телефон'}: ${name}. ${advice}`
    : `Нет ответа от ${platform === 'ios' ? 'iPhone' : 'телефона'}: ${name}. ${advice}`;
}

const reconnectAdvice = 30_000;

function rendererText(code: string, kind: string, screen: string, since: number | undefined): string {
  if (code === 'playback_failed')
    return kind === 'video'
      ? 'Экран не смог воспроизвести видео — возможно, телевизор не поддерживает его формат. Перейдите к следующему файлу.'
      : 'Экран не смог показать этот файл. Перейдите к следующему.';
  if (code === 'reconnecting' && since !== undefined && Date.now() - since >= reconnectAdvice)
    return `Не удаётся подключиться к экрану «${screen}». Проверьте, что телевизор включён и в той же сети Wi-Fi, затем нажмите «Перезапустить показ».`;
  return rendererErrors[code] ?? `Ошибка экрана: ${code}`;
}

const mbps = (bps: number) => {
  const v = bps / 1e6;
  return String(v >= 10 ? Math.round(v) : Math.round(v * 10) / 10).replace('.', ',');
};

function bufferingText({ buffering, stalled, need_bps, rate_bps }: ShowState): string | undefined {
  if (!buffering) return undefined;
  if (!stalled) return 'Видео подгружается…';
  if (need_bps && rate_bps && rate_bps < need_bps)
    return `Wi-Fi не успевает за видео: нужно ≈ ${mbps(need_bps)} Мбит/с, идёт ≈ ${mbps(rate_bps)}. Поднесите телефон ближе к роутеру или переходите к следующему файлу.`;
  return 'Видео долго подгружается.';
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
  const [pollError, setPollError] = useState<string>();
  const [ended, setEnded] = useState(false);
  const [reconnecting, setReconnecting] = useState<number>();
  const commands = useRef(0);
  const pins = usePins(api, pile.id);
  useKeepAwake();

  useEffect(() => {
    api.timeline(pile.id).then(setTimeline, (e: unknown) => setError(describe(e)));
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      const seen = commands.current;
      try {
        const st = await api.show();
        if (stopped) return;
        setPollError(undefined);
        if (seen === commands.current) setState(st);
      } catch (e) {
        if (stopped) return;
        if (e instanceof ApiError && e.code === 'no_show') {
          setEnded(true);
          return;
        }
        setPollError(describe(e));
      }
      timer = setTimeout(poll, 1000);
    };
    timer = setTimeout(poll, 1000);
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [api, pile.id]);

  const code = state.renderer_error?.code;
  useEffect(() => setReconnecting((at) => (code === 'reconnecting' ? (at ?? Date.now()) : undefined)), [code]);

  const byId = useMemo(() => new Map(timeline.map((i) => [i.id, i])), [timeline]);
  const names = useMemo(() => new Map(timeline.map((i) => [i.author, i.author_name])), [timeline]);

  async function run(command: () => Promise<ShowState>) {
    setError(undefined);
    commands.current++;
    try {
      setState(await command());
    } catch (e) {
      setError(describe(e));
    } finally {
      commands.current++;
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
  const hints = state.hints.filter((h) => h.reason !== 'disk_full');
  const full = hints.length < state.hints.length && !pins.progress.some((p) => p.reason === 'disk_full');
  const rendererError = state.renderer_error?.code === 'load_failed' && ownerAway ? undefined : state.renderer_error;

  if (ended) {
    return (
      <Screen title={`На экране «${state.screen.name}»`}>
        <ScrollView contentContainerStyle={styles.body}>
          <Text style={{ fontSize: 17, fontWeight: '600' }}>Показ завершён. Чтобы продолжить, начните показ снова.</Text>
          <Button title="Закрыть" onPress={onStop} />
        </ScrollView>
      </Screen>
    );
  }

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
              {videoText(state.video_reason, current.codec, author ?? '')}
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
        {bufferingText(state) && <Text>{bufferingText(state)}</Text>}
        {full && <ErrorText error={diskFull} />}
        {hints.length > 0 && (
          <View testID="waiting" style={{ backgroundColor: '#fff3cd', borderRadius: 8, padding: 12, gap: 8 }}>
            {hints.map((h) => (
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
          <ErrorText error={rendererText(rendererError.code, current.kind, state.screen.name, reconnecting)} />
        )}
        {rendererError?.code === 'reconnecting' && (
          <Button
            title="Перезапустить показ"
            onPress={() => run(() => api.startShow(pile.id, state.screen.id, current.id))}
          />
        )}
        <ErrorText error={error ?? pollError} />
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
