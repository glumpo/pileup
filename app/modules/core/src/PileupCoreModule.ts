import { NativeModule, requireNativeModule } from 'expo';

declare class PileupCoreModule extends NativeModule<{}> {
  start(): Promise<void>;
  stop(): Promise<void>;
  port(): number;
  token(): string;
  requestMediaPermissions(): Promise<boolean>;
}

export default requireNativeModule<PileupCoreModule>('PileupCore');
