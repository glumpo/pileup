import { Image } from 'expo-image';
import { useEffect, useMemo, useState } from 'react';
import { StyleProp, StyleSheet, View, ViewStyle } from 'react-native';

import { ImageSource } from './api';

function factors(nx: number, ny: number): number {
  let n = 0;
  for (let cy = 0; cy < ny; cy++) for (let cx = cy ? 0 : 1; cx * ny < nx * (ny - cy); cx++) n++;
  return n;
}

function placeholderOf(thumbhash: string): { thumbhash: string } | undefined {
  if (!thumbhash) return undefined;
  try {
    const hash = Uint8Array.from(atob(thumbhash), (c) => c.charCodeAt(0));
    if (hash.length < 5) throw new Error(`thumbhash too short: ${thumbhash}`);
    const alpha = (hash[2] & 0x80) !== 0;
    const landscape = (hash[4] & 0x80) !== 0;
    const side = hash[3] & 7;
    const fixed = alpha ? 5 : 7;
    const lx = Math.max(3, landscape ? fixed : side);
    const ly = Math.max(3, landscape ? side : fixed);
    const count = factors(lx, ly) + 2 * factors(3, 3) + (alpha ? factors(5, 5) : 0);
    if (side === 0 || hash.length < (alpha ? 6 : 5) + Math.ceil(count / 2))
      throw new Error(`thumbhash malformed: ${thumbhash}`);
    return { thumbhash };
  } catch (e) {
    console.warn('thumbhash', e);
    return undefined;
  }
}

export function Preview({
  thumbhash,
  source,
  style,
  resizeMode = 'cover',
}: {
  thumbhash: string;
  source: ImageSource;
  style: StyleProp<ViewStyle>;
  resizeMode?: 'cover' | 'contain';
}) {
  const [attempt, setAttempt] = useState(0);
  const [failed, setFailed] = useState(false);
  const placeholder = useMemo(() => placeholderOf(thumbhash), [thumbhash]);

  useEffect(() => {
    if (!failed) return;
    const timer = setTimeout(
      () => {
        setFailed(false);
        setAttempt((a) => a + 1);
      },
      Math.min(10_000 * 2 ** attempt, 120_000),
    );
    return () => clearTimeout(timer);
  }, [failed, attempt]);

  return (
    <View style={[styles.box, style]}>
      <Image
        key={attempt}
        testID="image"
        source={source}
        placeholder={placeholder}
        placeholderContentFit={resizeMode}
        contentFit={resizeMode}
        cachePolicy="memory"
        style={StyleSheet.absoluteFill}
        onError={() => setFailed(true)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  box: { backgroundColor: '#ddd', overflow: 'hidden' },
});
