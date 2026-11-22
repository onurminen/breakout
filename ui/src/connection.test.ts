import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { MarketConnection } from './connection';

class FakeSocket {
  readyState = 0;
  sent: string[] = [];
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  open() { this.readyState = 1; this.onopen?.(); }
  send(data: string) { this.sent.push(data); }
  close() { this.readyState = 3; this.onclose?.(); }
  receive(data: object) { this.onmessage?.({ data: JSON.stringify(data) }); }
}

describe('market connection', () => {
  beforeEach(() => { vi.useFakeTimers(); vi.stubGlobal('WebSocket', { OPEN: 1 }); });
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

  it('resubscribes after a disconnect and requests history after each confirmation', () => {
    const sockets: FakeSocket[] = [];
    const confirmed: string[] = [];
    const statuses: string[] = [];
    const connection = new MarketConnection({
      onStatus: (status) => statuses.push(status),
      onConfirmed: (symbol) => confirmed.push(symbol),
      onLive: () => {},
    }, () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket as unknown as WebSocket;
    });
    connection.setSymbols(['PX1.FR', 'PX2.FR']);
    connection.start();
    sockets[0].open();
    expect(JSON.parse(sockets[0].sent[0])).toEqual({ subscribe: ['PX1.FR', 'PX2.FR'] });
    sockets[0].receive({ type: 'subscribed', symbols: ['PX1.FR', 'PX2.FR'] });
    sockets[0].close();
    vi.advanceTimersByTime(1000);
    sockets[1].open();
    sockets[1].receive({ type: 'subscribed', symbols: ['PX1.FR', 'PX2.FR'] });
    expect(JSON.parse(sockets[1].sent[0])).toEqual({ subscribe: ['PX1.FR', 'PX2.FR'] });
    expect(confirmed).toEqual(['PX1.FR', 'PX2.FR', 'PX1.FR', 'PX2.FR']);
    expect(statuses).toEqual(['connecting', 'connected', 'reconnecting', 'connected']);
    connection.stop();
  });

  it('unsubscribes removed symbols and ignores their late frames', () => {
    const socket = new FakeSocket();
    const live: string[] = [];
    const connection = new MarketConnection({
      onStatus: () => {}, onConfirmed: () => {}, onLive: (message) => live.push(message.data.symbol),
    }, () => socket as unknown as WebSocket);
    connection.setSymbols(['PX1.FR']);
    connection.start();
    socket.open();
    connection.setSymbols([]);
    expect(JSON.parse(socket.sent[1])).toEqual({ unsubscribe: ['PX1.FR'] });
    socket.receive({ type: 'ema', data: { symbol: 'PX1.FR', windowStartMs: '300000' } });
    expect(live).toEqual([]);
    connection.stop();
  });
});
