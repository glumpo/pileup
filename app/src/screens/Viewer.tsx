import { useCallback, useEffect, useState } from 'react';
import { FlatList, Image, Pressable, StyleSheet, Text, useWindowDimensions, View, ViewStyle } from 'react-native';

import { Api, ApiError, Pile, TimelineItem } from '../api';
import { Preview } from '../Preview';
import { dayOf, duration, timeOf } from '../time';
import { Boundary, describe } from '../ui';

type Problem = { text: string; retry: boolean };

const advice = 'Возможно, погас экран — попросите разблокировать телефон и открыть Pileup.';

function problemOf(error: unknown, author: string): Problem {
  const code = error instanceof ApiError ? error.code : undefined;
  if (code === 'source_unavailable') return { text: `Нет ответа от участника: ${author}. ${advice}`, retry: true };
  if (code === 'source_failed')
    return {
      text: `Телефон участника не смог подготовить файл: ${author}. Возможно, файл удалён из галереи или в неподдерживаемом формате.`,
      retry: false,
    };
  if (code === 'render_failed')
    return {
      text: 'Не удалось открыть файл на этом телефоне. Возможно, он удалён из галереи или в неподдерживаемом формате.',
      retry: false,
    };
  if (code === 'unknown_asset') return { text: 'Файл удалён или изменён: обновите хронологию', retry: false };
  return { text: `Не удалось загрузить файл. ${describe(error)}`, retry: false };
}

function Page({ api, pile, item, style }: { api: Api; pile: string; item: TimelineItem; style: ViewStyle }) {
  const [attempt, setAttempt] = useState(0);
  const [problem, setProblem] = useState<Problem>();

  useEffect(() => {
    if (!problem?.retry) return;
    const timer = setTimeout(() => setAttempt((a) => a + 1), 2000);
    return () => clearTimeout(timer);
  }, [problem]);

  const failed = () =>
    api.checkBlob(pile, item, 2).then(
      () => setProblem({ text: 'Не удалось показать файл', retry: false }),
      (e) => setProblem(problemOf(e, item.author_name)),
    );

  return (
    <View style={style}>
      <Preview
        thumbhash={item.thumbhash}
        source={api.blob(pile, item, 1)}
        style={StyleSheet.absoluteFill}
        resizeMode="contain"
      />
      <Image
        key={attempt}
        testID="image"
        source={api.blob(pile, item, 2)}
        style={StyleSheet.absoluteFill}
        resizeMode="contain"
        onLoad={() => setProblem(undefined)}
        onError={failed}
      />
      {problem && <Text style={styles.problem}>{problem.text}</Text>}
    </View>
  );
}

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
    ({ item: page }: { item: TimelineItem }) => <Page api={api} pile={pile.id} item={page} style={{ width, height }} />,
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
          <Boundary item={item} fallback={<Text style={styles.text}>Не удалось показать подпись</Text>}>
            <Text style={styles.text}>{item.author_name}</Text>
            <Text style={styles.text}>
              {dayOf(item)} {timeOf(item)}
              {item.duration_ms !== undefined ? ` · видео ${duration(item.duration_ms)}` : ''}
            </Text>
          </Boundary>
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
  problem: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    padding: 16,
    paddingBottom: 32,
    color: '#fff',
    fontSize: 16,
    backgroundColor: 'rgba(0,0,0,0.6)',
  },
});
