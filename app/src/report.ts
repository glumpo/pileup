type Send = (kind: string, message: string) => Promise<unknown>;
type Handler = (error: unknown, fatal?: boolean) => void;

let send: Send | undefined;
let previous: Handler | undefined;

function text(error: unknown): string {
  return error instanceof Error ? (error.stack ?? `${error.name}: ${error.message}`) : String(error);
}

export function report(error: unknown, more = ''): Promise<unknown> {
  return (send?.('app', text(error) + more) ?? Promise.resolve()).catch(() => {});
}

function handle(error: unknown, fatal?: boolean) {
  const sent = report(error, fatal ? '\n(fatal)' : '');
  if (!fatal) return previous?.(error, fatal);
  Promise.race([sent, new Promise((resolve) => setTimeout(resolve, 1000))]).then(() => previous?.(error, fatal));
}

export function reportTo(to: Send) {
  send = to;
  const current = ErrorUtils.getGlobalHandler();
  if (current !== handle) {
    previous = current;
    ErrorUtils.setGlobalHandler(handle);
    if (!__DEV__) {
      (globalThis as { HermesInternal?: any }).HermesInternal?.enablePromiseRejectionTracker?.({
        allRejections: true,
        onUnhandled: (_id: number, error: unknown) => report(error, '\n(unhandled promise rejection)'),
      });
    }
  }
}
