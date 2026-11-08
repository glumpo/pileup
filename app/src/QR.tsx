import qrcode from 'qrcode-generator';
import { useMemo } from 'react';
import { View } from 'react-native';

export function QR({ text, size }: { text: string; size: number }) {
  const rows = useMemo(() => {
    const qr = qrcode(0, 'M');
    qr.addData(text, 'Alphanumeric');
    qr.make();
    const n = qr.getModuleCount();
    return Array.from({ length: n }, (_, r) => {
      const runs: { start: number; length: number }[] = [];
      for (let c = 0; c < n; c++) {
        if (!qr.isDark(r, c)) continue;
        const last = runs[runs.length - 1];
        if (last && last.start + last.length === c) last.length++;
        else runs.push({ start: c, length: 1 });
      }
      return runs;
    });
  }, [text]);
  const cell = Math.floor(size / (rows.length + 8));
  const side = cell * (rows.length + 8);
  return (
    <View
      accessible
      accessibilityRole="image"
      accessibilityLabel="QR-код приглашения"
      style={{ width: side, height: side, padding: cell * 4, backgroundColor: '#fff' }}
    >
      {rows.map((runs, r) => (
        <View key={r} style={{ height: cell }}>
          {runs.map((run) => (
            <View
              key={run.start}
              style={{ position: 'absolute', left: run.start * cell, width: run.length * cell, height: cell, backgroundColor: '#000' }}
            />
          ))}
        </View>
      ))}
    </View>
  );
}
