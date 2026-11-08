import { registerWebModule, NativeModule } from 'expo';

// PileupCoreModule is not available on the web platform.
class PileupCoreModule extends NativeModule<{}> {}

export default registerWebModule(PileupCoreModule, 'PileupCoreModule');
