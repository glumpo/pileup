import { NativeModule, requireNativeModule } from 'expo';

declare class PileupCoreModule extends NativeModule<{}> {
  hello(): string;
  setValueAsync(value: string): Promise<void>;
}

export default requireNativeModule<PileupCoreModule>('PileupCore');
