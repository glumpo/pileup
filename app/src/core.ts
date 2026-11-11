import PileupCore from '../modules/core/src/PileupCoreModule';
import { connect } from './api';

export async function startCore() {
  await PileupCore.start();
  return connect(PileupCore.port(), PileupCore.token());
}

export async function restartCore() {
  await PileupCore.stop().catch(() => {});
  return startCore();
}

export function requestMediaPermissions(): Promise<boolean> {
  return PileupCore.requestMediaPermissions();
}
