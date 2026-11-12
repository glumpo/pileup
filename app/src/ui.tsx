import { Component, ErrorInfo, ReactNode } from 'react';
import { Pressable, StyleSheet, Text, TextInput, TextInputProps, View } from 'react-native';

import { ApiError } from './api';
import { report } from './report';

const messages: Record<string, string> = {
  bad_invite: 'Это не приглашение Pileup',
  unknown_invite_version: 'Приглашение из более новой версии приложения',
  already_joined: 'Вы уже участник этого pile',
  local_network_denied: 'Нет доступа к локальной сети: откройте Настройки → Pileup и включите «Локальная сеть»',
  inviter_unreachable: 'Не удалось связаться с пригласившим: телефоны должны быть в одной сети',
  offline: 'Нет сети: подключитесь к Wi-Fi, чтобы пригласить',
  no_media: 'Медиатека недоступна',
  screen_unreachable: 'Экран не ответил: проверьте, что он включён и подключён к той же сети Wi-Fi',
  unknown_screen: 'Экран больше не найден в сети: обновите список',
  empty_pile: 'В pile пока нет файлов для показа',
  no_show: 'Показ уже остановлен',
  not_video: 'Сейчас на экране не видео',
  unknown_member: 'Участник не найден в pile',
  internal:
    'Что-то пошло не так в Pileup. Подробности записаны в «Журнал» на главном экране — отправьте его разработчику.',
  disk_full: 'На телефоне закончилось место. Освободите место и повторите.',
  media_failed: 'Нет доступа к медиатеке. Разрешите доступ к фото и видео в настройках телефона.',
};

const restarting = 'Pileup перезапускается после сбоя: подождите несколько секунд.';

export function describe(error: unknown): string {
  if (error instanceof ApiError && error.code === 'screen_unavailable' && error.reason) return error.reason;
  if (error instanceof ApiError && error.status === 401) return restarting;
  if (error instanceof ApiError) return messages[error.code] ?? `Ошибка ядра: ${error.code}`;
  if (error instanceof TypeError && error.message === 'Network request failed')
    return restarting;
  return String(error);
}

export function Screen({ title, onBack, children }: { title: string; onBack?: () => void; children: ReactNode }) {
  return (
    <View style={styles.screen}>
      <View style={styles.header}>
        {onBack && (
          <Pressable accessibilityRole="button" onPress={onBack} hitSlop={12}>
            <Text style={styles.back}>‹ Назад</Text>
          </Pressable>
        )}
        <Text style={styles.title} numberOfLines={1}>
          {title}
        </Text>
      </View>
      {children}
    </View>
  );
}

export function Button({
  title,
  label,
  onPress,
  disabled,
}: {
  title: string;
  label?: string;
  onPress: () => void;
  disabled?: boolean;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled }}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [styles.button, disabled && styles.disabled, pressed && styles.pressed]}
    >
      <Text style={styles.buttonText}>{title}</Text>
    </Pressable>
  );
}

export function Field(props: TextInputProps & { label: string }) {
  return (
    <TextInput
      accessibilityLabel={props.label}
      placeholder={props.label}
      autoCorrect={false}
      style={styles.field}
      {...props}
    />
  );
}

export class Boundary extends Component<
  { item: unknown; fallback: ReactNode; children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: unknown, info: ErrorInfo) {
    console.warn('render', error);
    report(error, info.componentStack ?? '');
  }

  componentDidUpdate(prev: { item: unknown }) {
    if (this.state.failed && prev.item !== this.props.item) this.setState({ failed: false });
  }

  render() {
    return this.state.failed ? this.props.fallback : this.props.children;
  }
}

export function ErrorText({ error }: { error?: string }) {
  return error ? <Text style={styles.error}>{error}</Text> : null;
}

export const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#fff', paddingTop: 48 },
  header: { flexDirection: 'row', alignItems: 'center', gap: 16, paddingHorizontal: 16, paddingBottom: 12 },
  back: { fontSize: 17, color: '#0a64d8' },
  title: { flex: 1, fontSize: 20, fontWeight: '600' },
  body: { padding: 16, gap: 12 },
  button: { backgroundColor: '#0a64d8', borderRadius: 8, padding: 12, alignItems: 'center' },
  disabled: { opacity: 0.4 },
  pressed: { opacity: 0.6 },
  buttonText: { color: '#fff', fontSize: 16 },
  field: { borderWidth: 1, borderColor: '#ccc', borderRadius: 8, padding: 10, fontSize: 16 },
  error: { color: '#c62828' },
  muted: { color: '#777' },
  row: { flexDirection: 'row', alignItems: 'center', paddingVertical: 12, paddingHorizontal: 16, gap: 12 },
});
