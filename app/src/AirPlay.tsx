import { useEffect, useState } from 'react';
import { Platform, Text, View } from 'react-native';

import PileupCore from '../modules/core/src/PileupCoreModule';

export function useExternalDisplay(): boolean | undefined {
  const [connected, setConnected] = useState<boolean>();
  useEffect(() => {
    if (Platform.OS !== 'ios') return;
    let live = true;
    const sub = PileupCore.addListener('onExternalDisplay', (e) => setConnected(e.connected));
    PileupCore.externalDisplay().then(
      (c) => live && setConnected(c),
      () => {},
    );
    return () => {
      live = false;
      sub.remove();
    };
  }, []);
  return connected;
}

const controlCenter =
  'откройте Пункт управления (смахните вниз от правого верхнего угла экрана, на iPhone с кнопкой «Домой» — вверх от нижнего края), нажмите «Повтор экрана» и выберите';

export function MirroringHint({ text }: { text: string }) {
  return (
    <View testID="mirroring" style={{ backgroundColor: '#fff3cd', borderRadius: 8, padding: 12 }}>
      <Text>{text}</Text>
    </View>
  );
}

export const pickerHint = `Фото на Apple TV показываются через «Повтор экрана». Сначала включите «Повтор экрана»: ${controlCenter} Apple TV. Затем выберите его здесь.`;

export const remoteHint = (screen: string) =>
  `На экране «${screen}» пока ничего нет: включите «Повтор экрана». Для этого ${controlCenter} «${screen}» — слайд появится на телевизоре.`;

export const keepOpen = 'Не блокируйте iPhone и не сворачивайте Pileup, пока идёт показ: иначе он прервётся на телевизоре.';
