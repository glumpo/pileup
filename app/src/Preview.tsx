import { useMemo, useState } from 'react';
import { Image, ImageResizeMode, StyleProp, StyleSheet, View, ViewStyle } from 'react-native';
import { thumbHashToDataURL } from 'thumbhash';

import { ImageSource } from './api';

function placeholderOf(thumbhash: string): string | undefined {
  if (!thumbhash) return undefined;
  try {
    return thumbHashToDataURL(Uint8Array.from(atob(thumbhash), (c) => c.charCodeAt(0)));
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
  resizeMode?: ImageResizeMode;
}) {
  const [loaded, setLoaded] = useState(false);
  const placeholder = useMemo(() => placeholderOf(thumbhash), [thumbhash]);
  return (
    <View style={[styles.box, style]}>
      {!loaded && placeholder && (
        <Image testID="placeholder" source={{ uri: placeholder }} style={StyleSheet.absoluteFill} resizeMode={resizeMode} />
      )}
      <Image
        testID="image"
        source={source}
        style={StyleSheet.absoluteFill}
        resizeMode={resizeMode}
        onLoad={() => setLoaded(true)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  box: { backgroundColor: '#ddd', overflow: 'hidden' },
});
