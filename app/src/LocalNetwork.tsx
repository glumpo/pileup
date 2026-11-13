import { useEffect, useState } from 'react';
import { Linking, Text, View } from 'react-native';

import { Api } from './api';
import { Button, styles } from './ui';

export function LocalNetworkBanner({ api }: { api: Api }) {
  const [denied, setDenied] = useState(false);

  useEffect(() => {
    const check = () => api.localNetwork().then((r) => setDenied(r.denied), () => setDenied(false));
    check();
    const timer = setInterval(check, 3000);
    return () => clearInterval(timer);
  }, [api]);

  if (!denied) return null;
  return (
    <View style={[styles.body, { backgroundColor: '#fdecea' }]}>
      <Text style={styles.error}>
        Pileup не может связаться с другими устройствами: запрещён доступ к локальной сети. Откройте Настройки → Pileup
        и включите «Локальная сеть».
      </Text>
      <Button title="Открыть Настройки" onPress={() => Linking.openSettings().catch(() => {})} />
    </View>
  );
}
