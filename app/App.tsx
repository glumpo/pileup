import { StatusBar } from 'expo-status-bar';
import { useCallback, useEffect, useRef, useState } from 'react';
import { AppState, BackHandler, Share, StyleSheet, Text, View } from 'react-native';

import { Api, Pile, ShowState, TimelineItem } from './src/api';
import { restartCore, startCore } from './src/core';
import { Join } from './src/screens/Join';
import { Log } from './src/screens/Log';
import { PileScreen } from './src/screens/PileScreen';
import { Piles } from './src/screens/Piles';
import { Remote } from './src/screens/Remote';
import { ScreenPicker } from './src/screens/ScreenPicker';
import { SelectionScreen } from './src/screens/SelectionScreen';
import { Timeline } from './src/screens/Timeline';
import { Viewer } from './src/screens/Viewer';
import { Boundary, Button, styles } from './src/ui';

type Route =
  | { name: 'piles' }
  | { name: 'join' }
  | { name: 'log' }
  | { name: 'pile'; pile: Pile }
  | { name: 'selection'; pile: Pile }
  | { name: 'timeline'; pile: Pile }
  | { name: 'viewer'; pile: Pile; items: TimelineItem[]; index: number }
  | { name: 'screens'; pile: Pile; asset?: string }
  | { name: 'show'; pile: Pile; state: ShowState };

function startFailure(error: string) {
  if (error.includes('device key'))
    return 'Pileup не может прочитать ключ этого телефона. Данные не удалены. Нажмите «Поделиться подробностями» и отправьте их разработчикам.';
  if (/no space left|disk is full|\(4874\)/.test(error))
    return 'На телефоне закончилось место. Освободите место и нажмите «Повторить».';
  return 'Не удалось запустить Pileup. Чаще всего причина — нехватка места на телефоне: освободите место и нажмите «Повторить».';
}

function StartFailure({ error, onRetry }: { error: string; onRetry: () => void }) {
  return (
    <View style={[styles.screen, styles.body, { justifyContent: 'center' }]}>
      <Text style={{ fontSize: 17 }}>{startFailure(error)}</Text>
      <Button title="Повторить" onPress={onRetry} />
      <Button
        title="Поделиться подробностями"
        onPress={() => Share.share({ title: 'Ошибка запуска Pileup', message: error }).catch(() => {})}
      />
    </View>
  );
}

function alive(api: Api) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return Promise.race([
    api.health().then(
      () => true,
      () => false,
    ),
    new Promise<boolean>((resolve) => {
      timer = setTimeout(() => resolve(false), 2000);
    }),
  ]).finally(() => clearTimeout(timer));
}

export default function App() {
  const [api, setApi] = useState<Api>();
  const [error, setError] = useState<string>();
  const [stack, setStack] = useState<Route[]>([{ name: 'piles' }]);
  const restarting = useRef(false);
  const checking = useRef(false);
  const lost = useRef(() => {});
  const onLost = useCallback(() => lost.current(), []);

  const start = useCallback(() => {
    setError(undefined);
    startCore(onLost).then((fresh) => {
      setApi(fresh);
      fresh
        .show()
        .then(async (state) => {
          const pile = (await fresh.piles()).find((p) => p.id === state.pile);
          if (pile) setStack([{ name: 'piles' }, { name: 'show', pile, state }]);
        })
        .catch(() => {});
    }, (e) => setError(String(e)));
  }, [onLost]);
  useEffect(start, [start]);

  const restart = useCallback(async () => {
    if (restarting.current) return;
    restarting.current = true;
    try {
      const fresh = await restartCore(onLost);
      setApi(fresh);
      return fresh;
    } catch (e) {
      setApi(undefined);
      setError(String(e));
    } finally {
      restarting.current = false;
    }
  }, [onLost]);

  lost.current = () => {
    if (!api || checking.current || restarting.current) return;
    checking.current = true;
    alive(api)
      .then(async (ok) => {
        const fresh = ok ? undefined : await restart();
        if (fresh && !(await alive(fresh))) {
          setApi(undefined);
          setError('core does not answer after restart');
        }
      })
      .finally(() => (checking.current = false));
  };

  const depth = stack.length;
  const showing = stack[depth - 1].name === 'show';
  useEffect(() => {
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      if (depth === 1) return false;
      if (!showing) setStack((s) => s.slice(0, -1));
      return true;
    });
    return () => sub.remove();
  }, [depth, showing]);

  useEffect(() => {
    if (!api) return;
    const sub = AppState.addEventListener('change', (state) => {
      if (state !== 'active') return;
      alive(api).then((ok) => void (ok ? api.reconnect().catch(() => {}) : restart()));
    });
    return () => sub.remove();
  }, [api, restart]);

  if (!api) {
    if (error) return <StartFailure error={error} onRetry={start} />;
    return (
      <View style={[styles.screen, { alignItems: 'center', justifyContent: 'center' }]}>
        <Text>Запуск ядра…</Text>
      </View>
    );
  }

  const push = (r: Route) => setStack((s) => [...s, r]);
  const replace = (r: Route) => setStack((s) => [...s.slice(0, -1), r]);
  const back = () => setStack((s) => s.slice(0, -1));
  const top = stack[stack.length - 1];

  const render = (route: Route) => {
    switch (route.name) {
      case 'piles':
        return (
          <Piles
            api={api}
            onOpen={(pile) => push({ name: 'pile', pile })}
            onJoin={() => push({ name: 'join' })}
            onLog={() => push({ name: 'log' })}
            onRestart={restart}
            onShow={(pile, state) => push({ name: 'show', pile, state })}
            onPick={(pile, asset) => push({ name: 'screens', pile, asset })}
          />
        );
      case 'log':
        return <Log api={api} onBack={back} />;
      case 'join':
        return <Join api={api} onBack={back} onJoined={(pile) => replace({ name: 'pile', pile })} />;
      case 'pile':
        return (
          <PileScreen
            api={api}
            pile={route.pile}
            onBack={back}
            onTimeline={(pile) => push({ name: 'timeline', pile })}
            onSelection={(pile) => push({ name: 'selection', pile })}
            onShow={(pile) => push({ name: 'screens', pile })}
          />
        );
      case 'selection':
        return <SelectionScreen api={api} pile={route.pile} onBack={back} />;
      case 'timeline':
        return (
          <Timeline
            api={api}
            pile={route.pile}
            onBack={back}
            onOpen={(items, index) => push({ name: 'viewer', pile: route.pile, items, index })}
          />
        );
      case 'viewer':
        return (
          <Viewer
            api={api}
            pile={route.pile}
            items={route.items}
            index={route.index}
            onBack={back}
            onShow={(item) => push({ name: 'screens', pile: route.pile, asset: item.id })}
          />
        );
      case 'screens':
        return (
          <ScreenPicker
            api={api}
            pile={route.pile}
            asset={route.asset}
            onBack={back}
            onStarted={(state) => replace({ name: 'show', pile: route.pile, state })}
          />
        );
      case 'show':
        return (
          <Remote
            api={api}
            pile={route.pile}
            initial={route.state}
            onStop={back}
            onPick={(asset) => replace({ name: 'screens', pile: route.pile, asset })}
          />
        );
    }
  };

  const visible = top.name === 'viewer' ? stack.slice(-2) : [top];
  return (
    <>
      <Boundary
        item={stack}
        fallback={
          <View style={[styles.screen, styles.body]}>
            <Text style={{ fontSize: 17 }}>Этот экран не удалось показать из-за ошибки в приложении.</Text>
            <Button title="К списку pile" onPress={() => setStack([{ name: 'piles' }])} />
          </View>
        }
      >
        {visible.map((route, i) => (
        <View
          key={stack.length - visible.length + i}
          style={StyleSheet.absoluteFill}
          accessibilityElementsHidden={route !== top}
          importantForAccessibility={route === top ? 'auto' : 'no-hide-descendants'}
        >
          {render(route)}
        </View>
        ))}
      </Boundary>
      <StatusBar style={top.name === 'viewer' ? 'light' : 'auto'} />
    </>
  );
}
