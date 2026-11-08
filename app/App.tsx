import { StatusBar } from 'expo-status-bar';
import { useEffect, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import PileupCore from './modules/core/src/PileupCoreModule';

async function checkHealth(): Promise<string> {
  await PileupCore.start();
  const url = `http://127.0.0.1:${PileupCore.port()}/health`;
  const withToken = await fetch(url, { headers: { Authorization: `Bearer ${PileupCore.token()}` } });
  const withoutToken = await fetch(url);
  return `with token: ${withToken.status} ${await withToken.text()}\nwithout token: ${withoutToken.status}`;
}

export default function App() {
  const [result, setResult] = useState('starting core…');

  useEffect(() => {
    checkHealth().then(setResult, (error) => setResult(`error: ${error}`));
  }, []);

  return (
    <View style={styles.container}>
      <Text>{result}</Text>
      <StatusBar style="auto" />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#fff',
    alignItems: 'center',
    justifyContent: 'center',
  },
});
