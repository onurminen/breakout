import { parseLiveMessage, type LiveMessage } from './market';

type ConnectionStatus = 'connecting' | 'connected' | 'reconnecting';

export type ConnectionCallbacks = {
  onStatus: (status: ConnectionStatus) => void;
  onConfirmed: (symbol: string) => void;
  onLive: (message: LiveMessage) => void;
};

export class MarketConnection {
  private socket: WebSocket | null = null;
  private selected = new Set<string>();
  private retry: ReturnType<typeof setTimeout> | null = null;
  private delayMs = 1000;
  private stopped = false;

  constructor(
    private readonly callbacks: ConnectionCallbacks,
    private readonly openSocket: () => WebSocket = () => {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      return new WebSocket(`${protocol}//${location.host}/ws`);
    },
  ) {}

  start(): void {
    this.callbacks.onStatus('connecting');
    this.connect();
  }

  setSymbols(symbols: readonly string[]): void {
    const next = new Set(symbols);
    const added = [...next].filter((symbol) => !this.selected.has(symbol));
    const removed = [...this.selected].filter((symbol) => !next.has(symbol));
    this.selected = next;
    if (this.socket?.readyState === WebSocket.OPEN) {
      if (removed.length) this.send({ unsubscribe: removed });
      if (added.length) this.send({ subscribe: added });
    }
  }

  stop(): void {
    this.stopped = true;
    if (this.retry !== null) clearTimeout(this.retry);
    this.socket?.close();
    this.socket = null;
  }

  private send(value: { subscribe: string[] } | { unsubscribe: string[] }): void {
    this.socket?.send(JSON.stringify(value));
  }

  private connect(): void {
    if (this.stopped) return;
    const socket = this.openSocket();
    this.socket = socket;
    socket.onopen = () => {
      if (this.socket !== socket) return;
      this.delayMs = 1000;
      this.callbacks.onStatus('connected');
      if (this.selected.size) this.send({ subscribe: [...this.selected] });
    };
    socket.onmessage = (event) => {
      if (this.socket !== socket) return;
      try {
        const raw: unknown = JSON.parse(String(event.data));
        if (typeof raw === 'object' && raw !== null && 'type' in raw && raw.type === 'subscribed' && 'symbols' in raw && Array.isArray(raw.symbols)) {
          for (const symbol of raw.symbols) {
            if (typeof symbol === 'string' && this.selected.has(symbol)) this.callbacks.onConfirmed(symbol);
          }
          return;
        }
        const message = parseLiveMessage(raw);
        if (message && this.selected.has(message.data.symbol)) this.callbacks.onLive(message);
      } catch {
        // Ignore a malformed frame; the next valid frame can still be processed.
      }
    };
    socket.onclose = () => {
      if (this.socket !== socket || this.stopped) return;
      this.socket = null;
      this.callbacks.onStatus('reconnecting');
      this.retry = setTimeout(() => {
        this.retry = null;
        this.connect();
      }, this.delayMs);
      this.delayMs = Math.min(this.delayMs * 2, 15_000);
    };
    socket.onerror = () => socket.close();
  }
}
