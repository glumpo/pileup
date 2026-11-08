import { NativeModule, requireNativeModule } from 'expo';

declare class PileupCoreModule extends NativeModule<{}> {
  start(): Promise<void>;
  stop(): Promise<void>;
  port(): number;
  token(): string;
}

export default requireNativeModule<PileupCoreModule>('PileupCore');
