import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { AppState, BackHandler, StyleSheet, Text, View } from 'react-native';

import { Api, Pile, ShowState, TimelineItem } from './src/api';
import { startCore } from './src/core';
import { Join } from './src/screens/Join';
import { Log } from './src/screens/Log';
import { PileScreen } from './src/screens/PileScreen';
import { Piles } from './src/screens/Piles';
import { Remote } from './src/screens/Remote';
import { ScreenPicker } from './src/screens/ScreenPicker';
import { SelectionScreen } from './src/screens/SelectionScreen';
import { Timeline } from './src/screens/Timeline';
import { Viewer } from './src/screens/Viewer';
import { styles } from './src/ui';

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

export default function App() {
  const [api, setApi] = useState<Api>();
  const [error, setError] = useState<string>();
  const [stack, setStack] = useState<Route[]>([{ name: 'piles' }]);

  useEffect(() => {
    startCore().then(setApi, (e) => setError(String(e)));
  }, []);

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
      if (state === 'active') api.reconnect().catch(() => {});
    });
    return () => sub.remove();
  }, [api]);

  if (!api) {
    return (
      <View style={[styles.screen, { alignItems: 'center', justifyContent: 'center' }]}>
        <Text>{error ?? 'Запуск ядра…'}</Text>
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
        return <Remote api={api} pile={route.pile} initial={route.state} onStop={back} />;
    }
  };

  const visible = top.name === 'viewer' ? stack.slice(-2) : [top];
  return (
    <>
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
      <StatusBar style={top.name === 'viewer' ? 'light' : 'auto'} />
    </>
  );
}
