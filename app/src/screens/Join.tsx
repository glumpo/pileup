import { CameraView, useCameraPermissions } from 'expo-camera';
import { useState } from 'react';
import { ActivityIndicator, Linking, ScrollView, StyleSheet, Text } from 'react-native';

import { Api, ApiError, Pile } from '../api';
import { Button, describe, ErrorText, Field, Screen, styles } from '../ui';
import { LocalNetworkBanner } from '../LocalNetwork';

const unreachable: Record<string, string> = {
  refused: 'Пригласивший телефон не принимает подключение: попросите открыть Pileup на экране с приглашением.',
  timeout:
    'Пригласивший телефон не отвечает. Проверьте, что телефоны в одной сети Wi-Fi. Сеть отеля или гостевая сеть может не пускать телефоны друг к другу — тогда подключитесь к точке доступа с одного из телефонов.',
  no_route: 'Телефоны в разных сетях: подключитесь к той же сети Wi-Fi, что и пригласивший.',
  other_device: 'Приглашение устарело или принадлежит другому pile: попросите показать приглашение ещё раз.',
  error:
    'Телефон по адресу из приглашения не принял подключение: возможно, приглашение устарело — попросите показать его ещё раз.',
};

function problemOf(error: unknown, invite: string): string {
  const code = error instanceof ApiError ? error.code : undefined;
  const reason = error instanceof ApiError ? error.reason : undefined;
  if (code === 'inviter_unreachable' && reason && unreachable[reason]) return unreachable[reason];
  if (code === 'offline') return 'Этот телефон не подключён к Wi-Fi: подключитесь к той же сети, что и пригласивший.';
  if (code === 'bad_invite' && invite.startsWith('PILEUP:'))
    return 'Приглашение повреждено или скопировано не полностью: скопируйте его ещё раз.';
  return describe(error);
}

export function Join({ api, onBack, onJoined }: { api: Api; onBack: () => void; onJoined: (pile: Pile) => void }) {
  const [permission, requestPermission] = useCameraPermissions();
  const [scanning, setScanning] = useState(false);
  const [invite, setInvite] = useState('');
  const [member, setMember] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [blocked, setBlocked] = useState(false);

  async function scan() {
    setError(undefined);
    const camera = permission?.granted ? permission : await requestPermission();
    setBlocked(!camera.granted && !camera.canAskAgain);
    if (camera.granted) setScanning(true);
    else if (camera.canAskAgain) setError('Нет доступа к камере: вставьте приглашение текстом');
    else setError('Нет доступа к камере. Разрешите его в настройках телефона или вставьте приглашение текстом.');
  }

  async function join() {
    setBusy(true);
    setError(undefined);
    try {
      onJoined(await api.join(invite.trim(), member.trim()));
    } catch (e) {
      setError(problemOf(e, invite.trim()));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen title="Войти в pile" onBack={onBack}>
      <LocalNetworkBanner api={api} />
      <ScrollView contentContainerStyle={styles.body} keyboardShouldPersistTaps="handled">
        {scanning ? (
          <CameraView
            style={camera.view}
            facing="back"
            barcodeScannerSettings={{ barcodeTypes: ['qr'] }}
            onBarcodeScanned={({ data }) => {
              if (!data.startsWith('PILEUP:')) {
                setError('Это не QR-код приглашения Pileup. Наведите камеру на QR на экране пригласившего.');
                return;
              }
              setError(undefined);
              setInvite(data);
              setScanning(false);
            }}
          />
        ) : (
          <Button title="Сканировать QR" onPress={scan} />
        )}
        {blocked && <Button title="Открыть настройки" onPress={() => Linking.openSettings()} />}
        <Text style={styles.muted}>или вставьте приглашение текстом</Text>
        <Field label="Приглашение" value={invite} onChangeText={setInvite} autoCapitalize="characters" multiline />
        <Field label="Ваше имя" value={member} onChangeText={setMember} />
        <Button title="Войти" onPress={join} disabled={busy || !invite.trim() || !member.trim()} />
        {busy && (
          <>
            <ActivityIndicator testID="joining" />
            <Text style={styles.muted}>Подключаемся к пригласившему — это может занять до 30 секунд.</Text>
          </>
        )}
        <ErrorText error={error} />
      </ScrollView>
    </Screen>
  );
}

const camera = StyleSheet.create({
  view: { width: '100%', aspectRatio: 1, borderRadius: 8, overflow: 'hidden' },
});
