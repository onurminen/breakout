import { fromJson, type JsonValue } from '@bufbuild/protobuf';
import {
  AdvisorySchema,
  EmaResultSchema,
  type Advisory,
  type EmaResult,
} from './gen/breakout/results/v1/results_pb';

export type LiveMessage =
  | { type: 'ema'; data: EmaResult }
  | { type: 'advisory'; data: Advisory };

export type History = { emaResults: EmaResult[]; advisories: Advisory[] };

export type SymbolSnapshot = {
  results: Map<bigint, EmaResult>;
  advisories: Map<bigint, Advisory>;
  persistedThroughMs: bigint | null;
  loading: boolean;
  error: string | null;
};

export function emptySnapshot(): SymbolSnapshot {
  return {
    results: new Map(),
    advisories: new Map(),
    persistedThroughMs: null,
    loading: true,
    error: null,
  };
}

export function parseLiveMessage(raw: unknown): LiveMessage | null {
  if (typeof raw !== 'object' || raw === null || !('type' in raw) || !('data' in raw)) return null;
  const message = raw as { type: unknown; data: unknown };
  if (message.type === 'ema') return { type: 'ema', data: fromJson(EmaResultSchema, message.data as JsonValue) };
  if (message.type === 'advisory') return { type: 'advisory', data: fromJson(AdvisorySchema, message.data as JsonValue) };
  return null;
}

export function applyLive(snapshot: SymbolSnapshot, message: LiveMessage): SymbolSnapshot {
  if (message.type === 'ema') {
    const results = new Map(snapshot.results);
    results.set(message.data.windowStartMs, message.data);
    return { ...snapshot, results };
  }
  const advisories = new Map(snapshot.advisories);
  advisories.set(message.data.windowStartMs, message.data);
  return { ...snapshot, advisories };
}

export function mergeHistory(snapshot: SymbolSnapshot, history: History, buffered: LiveMessage[]): SymbolSnapshot {
  const results = new Map(snapshot.results);
  const advisories = new Map(snapshot.advisories);
  let persistedThroughMs = snapshot.persistedThroughMs;

  for (const result of history.emaResults) {
    if (!results.has(result.windowStartMs)) results.set(result.windowStartMs, result);
    if (persistedThroughMs === null || result.windowStartMs > persistedThroughMs) persistedThroughMs = result.windowStartMs;
  }
  for (const advisory of history.advisories) {
    if (!advisories.has(advisory.windowStartMs)) advisories.set(advisory.windowStartMs, advisory);
    if (persistedThroughMs === null || advisory.windowStartMs > persistedThroughMs) persistedThroughMs = advisory.windowStartMs;
  }

  let merged: SymbolSnapshot = { ...snapshot, results, advisories, persistedThroughMs, loading: false, error: null };
  for (const message of buffered) merged = applyLive(merged, message);
  return merged;
}

export async function fetchHistory(symbol: string, fromMs: bigint, signal: AbortSignal): Promise<History> {
  const params = new URLSearchParams({ symbol, from: fromMs.toString(), to: '9999999999999' });
  const response = await fetch(`/api/history?${params}`, { signal });
  if (!response.ok) throw new Error(`History request failed (${response.status})`);
  const body: unknown = await response.json();
  if (typeof body !== 'object' || body === null || !('emaResults' in body) || !('advisories' in body)) {
    throw new Error('Invalid history response');
  }
  const value = body as { emaResults: unknown; advisories: unknown };
  if (!Array.isArray(value.emaResults) || !Array.isArray(value.advisories)) throw new Error('Invalid history response');
  return {
    emaResults: value.emaResults.map((item) => fromJson(EmaResultSchema, item)),
    advisories: value.advisories.map((item) => fromJson(AdvisorySchema, item)),
  };
}
