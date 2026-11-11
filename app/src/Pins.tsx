import { useCallback, useEffect, useState } from 'react';
import { Text, View } from 'react-native';

import { Api, PinEstimate, PinProgress } from './api';
import { Button, describe, ErrorText, styles } from './ui';

export type Source = { author: string; name: string };

export const diskFull =
  'На этом телефоне закончилось место — загрузка остановлена. Освободите место или снимите закрепления.';

export function size(bytes: number): string {
  const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const value = i > 0 && n < 10 ? String(Number(n.toFixed(1))).replace('.', ',') : String(Math.round(n));
  return `${value} ${units[i]}`;
}

export function usePins(api: Api, pile: string) {
  const [progress, setProgress] = useState<PinProgress[]>([]);
  const [offer, setOffer] = useState<{ member?: Source; estimate: PinEstimate }>();
  const [error, setError] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const loading = progress.some((p) => p.done + p.failed < p.total);

  const refresh = useCallback(
    () =>
      api.pins(pile).then(
        (p) => {
          setProgress(p);
          setFailure(undefined);
        },
        (e) => setFailure(describe(e)),
      ),
    [api, pile],
  );

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!loading) return;
    const timer = setInterval(refresh, 1000);
    return () => clearInterval(timer);
  }, [refresh, loading]);

  async function run(action: () => Promise<void>) {
    setError(undefined);
    try {
      await action();
    } catch (e) {
      setError(describe(e));
    }
  }

  return {
    progress,
    offer,
    error: error ?? failure,
    ask: (member?: Source) =>
      run(async () => setOffer({ member, estimate: await api.pinEstimate(pile, member?.author) })),
    confirm: () =>
      run(async () => {
        setProgress(await api.pin(pile, offer?.member?.author));
        setOffer(undefined);
      }),
    cancel: () => setOffer(undefined),
    unpin: (member?: string) => run(async () => setProgress(await api.unpin(pile, member))),
  };
}

export function PinsPanel({ pins }: { pins: ReturnType<typeof usePins> }) {
  const { offer, progress } = pins;
  const estimate = offer?.estimate;
  return (
    <>
      {estimate && (
        <View testID="pin-offer" style={{ backgroundColor: '#eef4fd', borderRadius: 8, padding: 12, gap: 8 }}>
          <Text style={{ fontWeight: '600' }}>
            {offer?.member ? `Загрузить всё с участника: ${offer.member.name}` : 'Загрузить вообще всё'}
          </Text>
          <Text>
            {`Файлов: ${estimate.files}, ещё не загружено: ${estimate.missing} (примерно ${size(estimate.estimated_bytes)}).`}
          </Text>
          {estimate.free_bytes !== null && <Text>{`Свободно на устройстве: ${size(estimate.free_bytes)}.`}</Text>}
          {estimate.over_budget && (
            <Text style={styles.error}>
              {`Не поместится в кеш (${size(estimate.budget_bytes)}): закреплённые файлы всё равно загрузятся и займут место сверх него.`}
            </Text>
          )}
          <Text style={styles.muted}>
            Файлы останутся на устройстве, пока вы не снимете закрепление. Видео не загружается.
          </Text>
          <Button title="Начать загрузку" onPress={pins.confirm} />
          <Button title="Отмена" onPress={pins.cancel} />
        </View>
      )}
      <ErrorText error={pins.error} />
      {progress.some((p) => p.reason === 'disk_full') && <ErrorText error={diskFull} />}
      {progress.map((p) => (
        <View key={p.author} style={{ flexDirection: 'row', alignItems: 'center', gap: 12 }}>
          <Text style={{ flex: 1 }}>
            {`${p.name}: ${p.done}/${p.total}${p.failed > 0 ? `, ошибок: ${p.failed}` : ''}`}
          </Text>
          <Button
            title="Снять закрепление"
            label={`Снять закрепление: ${p.name}`}
            onPress={() => pins.unpin(p.author)}
          />
        </View>
      ))}
      {progress.length > 0 && <Button title="Снять все закрепления" onPress={() => pins.unpin()} />}
    </>
  );
}
