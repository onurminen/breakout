import { useEffect, useRef, useState } from 'react';
import { MarketConnection } from './connection';
import { applyLive, emptySnapshot, fetchHistory, mergeHistory, type LiveMessage, type SymbolSnapshot } from './market';
import type { Advisory, EmaResult } from './gen/breakout/results/v1/results_pb';

type Pending = { abort: AbortController; buffered: LiveMessage[] };
export type Latest = { result?: EmaResult; advisory?: Advisory };

// Live values for every selected symbol; history only for the focused one, so
// selecting thousands of symbols costs one history request, not thousands.
export function useMarketData(selected: readonly string[], focused: string | null) {
  const [snapshot, setSnapshot] = useState<SymbolSnapshot | null>(null);
  const [latest, setLatest] = useState<Record<string, Latest>>({});
  const [advisories, setAdvisories] = useState<Advisory[]>([]);
  const [status, setStatus] = useState<'connecting' | 'connected' | 'reconnecting'>('connecting');
  const statusRef = useRef(status);
  statusRef.current = status;
  const focusedRef = useRef<string | null>(null);
  const snapshotRef = useRef<SymbolSnapshot | null>(null);
  const pendingRef = useRef<Pending | null>(null);
  const latestRef = useRef<Record<string, Latest>>({});
  const advisoriesRef = useRef(new Map<string, Advisory>());
  const flushTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const connectionRef = useRef<MarketConnection | null>(null);

  // A window close brings one message per symbol at once; render them together.
  function flush() {
    if (flushTimer.current !== null) clearTimeout(flushTimer.current);
    flushTimer.current = null;
    setLatest({ ...latestRef.current });
    setAdvisories([...advisoriesRef.current.values()]);
  }

  function update(symbol: string, change: (snapshot: SymbolSnapshot) => SymbolSnapshot) {
    if (focusedRef.current !== symbol) return;
    snapshotRef.current = change(snapshotRef.current ?? emptySnapshot());
    setSnapshot(snapshotRef.current);
  }

  function sync(symbol: string, restart = false) {
    if (focusedRef.current !== symbol) return;
    const previous = pendingRef.current;
    if (previous) {
      if (!restart) return;
      previous.abort.abort();
    }
    const pending: Pending = { abort: new AbortController(), buffered: [] };
    pendingRef.current = pending;
    const fromMs = snapshotRef.current?.persistedThroughMs ?? 0n;
    update(symbol, (snapshot) => ({ ...snapshot, loading: snapshot.results.size === 0, error: null }));
    fetchHistory(symbol, fromMs, pending.abort.signal)
      .then((history) => {
        if (pendingRef.current !== pending) return;
        update(symbol, (snapshot) => mergeHistory(snapshot, history, pending.buffered));
      })
      .catch((error: unknown) => {
        if (pendingRef.current !== pending || pending.abort.signal.aborted) return;
        update(symbol, (snapshot) => {
          let next: SymbolSnapshot = { ...snapshot, loading: false, error: error instanceof Error ? error.message : 'History unavailable' };
          for (const message of pending.buffered) next = applyLive(next, message);
          return next;
        });
      })
      .finally(() => {
        if (pendingRef.current === pending) pendingRef.current = null;
      });
  }

  useEffect(() => {
    const connection = new MarketConnection({
      onStatus: setStatus,
      onConfirmed: (symbol) => sync(symbol, true),
      onLive: (message) => {
        const symbol = message.data.symbol;
        const current = latestRef.current[symbol] ?? {};
        if (message.type === 'ema') {
          if (!current.result || message.data.windowStartMs >= current.result.windowStartMs) latestRef.current[symbol] = { ...current, result: message.data };
        } else {
          if (!current.advisory || message.data.windowStartMs >= current.advisory.windowStartMs) latestRef.current[symbol] = { ...current, advisory: message.data };
          advisoriesRef.current.set(`${symbol}:${message.data.windowStartMs}`, message.data);
        }
        flushTimer.current ??= setTimeout(flush, 250);

        if (symbol !== focusedRef.current) return;
        if (pendingRef.current) pendingRef.current.buffered.push(message);
        else update(symbol, (snapshot) => applyLive(snapshot, message));
      },
    });
    connectionRef.current = connection;
    connection.start();
    const refresh = setInterval(() => {
      if (statusRef.current === 'connected' && focusedRef.current) sync(focusedRef.current);
    }, 10_000);
    return () => {
      clearInterval(refresh);
      if (flushTimer.current !== null) clearTimeout(flushTimer.current);
      flushTimer.current = null;
      connection.stop();
      connectionRef.current = null;
      pendingRef.current?.abort.abort();
      pendingRef.current = null;
    };
  }, []);

  useEffect(() => {
    const keep = new Set(selected);
    for (const symbol of Object.keys(latestRef.current)) if (!keep.has(symbol)) delete latestRef.current[symbol];
    for (const [key, advisory] of advisoriesRef.current) if (!keep.has(advisory.symbol)) advisoriesRef.current.delete(key);
    flush();
    connectionRef.current?.setSymbols(selected);
  }, [selected]);

  useEffect(() => {
    pendingRef.current?.abort.abort();
    pendingRef.current = null;
    focusedRef.current = focused;
    snapshotRef.current = focused ? emptySnapshot() : null;
    setSnapshot(snapshotRef.current);
    // A newly selected symbol syncs again once the API confirms its subscription.
    if (focused && statusRef.current === 'connected') sync(focused);
  }, [focused]);

  return { snapshot, latest, advisories, status };
}
