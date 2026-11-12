import { useKeepAwake } from 'expo-keep-awake';
import { useEffect, useState } from 'react';
import { AppState, StyleSheet, Text, View } from 'react-native';

import { Api } from './api';

const poll = 3000;
const margin = 60_000;
const tag = 'serving';

export function ServingBanner({ api }: { api: Api }) {
  const [serving, setServing] = useState(false);

  useEffect(() => {
    let live = true;
    let active = true;
    let until = 0;
    let timer: ReturnType<typeof setTimeout>;
    const sub = AppState.addEventListener('change', (state) => (active = state === 'active'));
    const check = async () => {
      if (active && (await api.serving().catch(() => false))) until = Date.now() + margin;
      if (!live) return;
      setServing(Date.now() < until);
      timer = setTimeout(check, poll);
    };
    check();
    return () => {
      live = false;
      clearTimeout(timer);
      sub.remove();
    };
  }, [api]);

  return serving ? <Banner /> : null;
}

function Banner() {
  useKeepAwake(tag);
  return (
    <View pointerEvents="none" style={banner.box}>
      <Text style={banner.text}>Не блокируйте телефон и не сворачивайте Pileup: с него идёт видео на экран.</Text>
    </View>
  );
}

const banner = StyleSheet.create({
  box: { position: 'absolute', left: 12, right: 12, bottom: 32, backgroundColor: '#fff4d6', borderRadius: 8, padding: 12 },
  text: { fontSize: 15, color: '#5c4400' },
});
