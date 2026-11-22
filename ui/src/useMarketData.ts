import { useEffect, useRef, useState } from 'react';
import { MarketConnection } from './connection';
import { applyLive, emptySnapshot, fetchHistory, mergeHistory, type LiveMessage, type SymbolSnapshot } from './market';

type Pending = { abort: AbortController; buffered: LiveMessage[] };

export function useMarketData(selected: readonly string[]) {
  const [snapshots, setSnapshots] = useState<Record<string, SymbolSnapshot>>({});
  const [status, setStatus] = useState<'connecting' | 'connected' | 'reconnecting'>('connecting');
  const snapshotsRef = useRef<Record<string, SymbolSnapshot>>({});
  const selectedRef = useRef(new Set(selected));
  const pendingRef = useRef(new Map<string, Pending>());
  const connectionRef = useRef<MarketConnection | null>(null);

  function update(symbol: string, change: (snapshot: SymbolSnapshot) => SymbolSnapshot) {
    if (!selectedRef.current.has(symbol)) return;
    const next = { ...snapshotsRef.current, [symbol]: change(snapshotsRef.current[symbol] ?? emptySnapshot()) };
    snapshotsRef.current = next;
    setSnapshots(next);
  }

  useEffect(() => {
    function sync(symbol: string, restart = false) {
      if (!selectedRef.current.has(symbol)) return;
      const previous = pendingRef.current.get(symbol);
      if (previous) {
        if (!restart) return;
        previous.abort.abort();
      }
      const pending: Pending = { abort: new AbortController(), buffered: [] };
      pendingRef.current.set(symbol, pending);
      const fromMs = snapshotsRef.current[symbol]?.persistedThroughMs ?? 0n;
      update(symbol, (snapshot) => ({ ...snapshot, loading: snapshot.results.size === 0, error: null }));
      fetchHistory(symbol, fromMs, pending.abort.signal)
        .then((history) => {
          if (pendingRef.current.get(symbol) !== pending) return;
          update(symbol, (snapshot) => mergeHistory(snapshot, history, pending.buffered));
        })
        .catch((error: unknown) => {
          if (pendingRef.current.get(symbol) !== pending || pending.abort.signal.aborted) return;
          update(symbol, (snapshot) => {
            let next: SymbolSnapshot = { ...snapshot, loading: false, error: error instanceof Error ? error.message : 'History unavailable' };
            for (const message of pending.buffered) next = applyLive(next, message);
            return next;
          });
        })
        .finally(() => {
          if (pendingRef.current.get(symbol) === pending) pendingRef.current.delete(symbol);
        });
    }

    const connection = new MarketConnection({
      onStatus: setStatus,
      onConfirmed: (symbol) => sync(symbol, true),
      onLive: (message) => {
        const pending = pendingRef.current.get(message.data.symbol);
        if (pending) pending.buffered.push(message);
        else update(message.data.symbol, (snapshot) => applyLive(snapshot, message));
      },
    });
    connectionRef.current = connection;
    connection.setSymbols([...selectedRef.current]);
    connection.start();
    const refresh = setInterval(() => {
      if (statusRef.current !== 'connected') return;
      for (const symbol of selectedRef.current) sync(symbol);
    }, 10_000);
    return () => {
      clearInterval(refresh);
      connection.stop();
      connectionRef.current = null;
      for (const pending of pendingRef.current.values()) pending.abort.abort();
      pendingRef.current.clear();
    };
  }, []);

  const statusRef = useRef(status);
  statusRef.current = status;

  useEffect(() => {
    const nextSelected = new Set(selected);
    for (const symbol of selectedRef.current) {
      if (!nextSelected.has(symbol)) pendingRef.current.get(symbol)?.abort.abort();
    }
    selectedRef.current = nextSelected;
    const nextSnapshots: Record<string, SymbolSnapshot> = {};
    for (const symbol of selected) nextSnapshots[symbol] = snapshotsRef.current[symbol] ?? emptySnapshot();
    snapshotsRef.current = nextSnapshots;
    setSnapshots(nextSnapshots);
    connectionRef.current?.setSymbols(selected);
  }, [selected]);

  return { snapshots, status };
}
