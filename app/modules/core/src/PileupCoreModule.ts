import { NativeModule, requireNativeModule } from 'expo';

export type Presented = { asset: string; image: string; video?: string; around: string[]; playing: boolean };

export type PlayerEvent = { asset: string; state: 'playing' | 'buffering' | 'ended' | 'failed'; message?: string };

type Events = {
  onExternalDisplay: (event: { connected: boolean }) => void;
  onPlayer: (event: PlayerEvent) => void;
};

declare class PileupCoreModule extends NativeModule<Events> {
  start(): Promise<void>;
  stop(): Promise<void>;
  port(): number;
  token(): string;
  requestMediaPermissions(): Promise<boolean>;
  present(slide: Presented | null): Promise<void>;
  externalDisplay(): Promise<boolean>;
}

export default requireNativeModule<PileupCoreModule>('PileupCore');
