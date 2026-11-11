import PileupCore from '../modules/core/src/PileupCoreModule';
import { connect } from './api';

export async function startCore(lost?: () => void) {
  await PileupCore.start();
  return connect(PileupCore.port(), PileupCore.token(), lost);
}

export async function restartCore(lost?: () => void) {
  await PileupCore.stop().catch(() => {});
  return startCore(lost);
}

export function requestMediaPermissions(): Promise<boolean> {
  return PileupCore.requestMediaPermissions();
}
