import { CameraView, useCameraPermissions } from 'expo-camera';
import { useState } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';

import { Api, Pile } from '../api';
import { Button, describe, ErrorText, Field, Screen, styles } from '../ui';

export function Join({ api, onBack, onJoined }: { api: Api; onBack: () => void; onJoined: (pile: Pile) => void }) {
  const [permission, requestPermission] = useCameraPermissions();
  const [scanning, setScanning] = useState(false);
  const [invite, setInvite] = useState('');
  const [member, setMember] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();

  async function scan() {
    setError(undefined);
    if (permission?.granted || (await requestPermission()).granted) setScanning(true);
    else setError('Нет доступа к камере: вставьте приглашение текстом');
  }

  async function join() {
    setBusy(true);
    setError(undefined);
    try {
      onJoined(await api.join(invite.trim(), member.trim()));
    } catch (e) {
      setError(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen title="Войти в pile" onBack={onBack}>
      <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled">
        {scanning ? (
          <CameraView
            style={camera.view}
            facing="back"
            barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
            onBarcodeScanned={({ data }) => {
              if (!data.startsWith('PILEUP:')) return;
              setInvite(data);
              setScanning(false);
            }}
          />
        ) : (
          <Button title="Сканировать QR" onPress={scan} />
        )}
        <Text style={styles.muted}>или вставьте приглашение текстом</Text>
        <Field label="Приглашение" value={invite} onChangeText={setInvite} autoCapitalize="characters" multiline />
        <Field label="Ваше имя" value={member} onChangeText={setMember} />
        <Button title="Войти" onPress={join} disabled={busy || !invite.trim() || !member.trim()} />
        <ErrorText error={error} />
      </ScrollView>
    </Screen>
  );
}

const camera = StyleSheet.create({
  view: { width: '100%', aspectRatio: 1, borderRadius: 8, overflow: 'hidden' },
});
