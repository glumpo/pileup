import { useCallback, useState } from 'react';
import { FlatList, Pressable, StyleSheet, Text, useWindowDimensions, View } from 'react-native';

import { Api, Pile, TimelineItem } from '../api';
import { Preview } from '../Preview';
import { dayOf, duration, timeOf } from '../time';

export function Viewer({
  api,
  pile,
  items,
  index,
  onBack,
  onShow,
}: {
  api: Api;
  pile: Pile;
  items: TimelineItem[];
  index: number;
  onBack: () => void;
  onShow: (item: TimelineItem) => void;
}) {
  const { width, height } = useWindowDimensions();
  const [current, setCurrent] = useState(index);
  const item = items[current];
  const renderPage = useCallback(
    ({ item: page }: { item: TimelineItem }) => (
      <Preview
        thumbhash={page.thumbhash}
        source={api.blob(pile.id, page, 2)}
        style={{ width, height }}
        resizeMode="contain"
      />
    ),
    [api, pile.id, width, height],
  );

  return (
    <View style={styles.screen}>
      <FlatList
        testID="viewer"
        data={items}
        horizontal
        pagingEnabled
        showsHorizontalScrollIndicator={false}
        initialScrollIndex={index}
        initialNumToRender={1}
        maxToRenderPerBatch={2}
        windowSize={3}
        keyExtractor={(i) => i.id}
        getItemLayout={(_, i) => ({ length: width, offset: width * i, index: i })}
        onMomentumScrollEnd={(e) => setCurrent(Math.round(e.nativeEvent.contentOffset.x / width))}
        renderItem={renderPage}
      />
      <View style={styles.caption}>
        <Pressable accessibilityRole="button" onPress={onBack} hitSlop={12}>
          <Text style={styles.text}>✕</Text>
        </Pressable>
        <View style={{ flex: 1 }}>
          <Text style={styles.text}>{item.author_name}</Text>
          <Text style={styles.text}>
            {dayOf(item)} {timeOf(item)}
            {item.duration_ms !== undefined ? ` · видео ${duration(item.duration_ms)}` : ''}
          </Text>
        </View>
        <Text style={styles.text}>
          {current + 1} / {items.length}
        </Text>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Показать на экране"
          onPress={() => onShow(item)}
          hitSlop={12}
        >
          <Text style={styles.text}>На экран</Text>
        </Pressable>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#000' },
  caption: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    paddingTop: 48,
    paddingHorizontal: 16,
    paddingBottom: 12,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 16,
    backgroundColor: 'rgba(0,0,0,0.4)',
  },
  text: { color: '#fff', fontSize: 16 },
});
