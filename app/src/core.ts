import PileupCore from '../modules/core/src/PileupCoreModule';
import { connect } from './api';

export async function startCore() {
  await PileupCore.start();
  return connect(PileupCore.port(), PileupCore.token());
}

export function requestMediaPermissions(): Promise<boolean> {
  return (PileupCore as unknown as { requestMediaPermissions(): Promise<boolean> }).requestMediaPermissions();
}
