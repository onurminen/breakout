import { describe, expect, it } from 'vitest';
import { fromJson } from '@bufbuild/protobuf';
import { AdvisorySchema, EmaResultSchema, Side } from './gen/breakout/results/v1/results_pb';
import { applyLive, emptySnapshot, mergeHistory, parseLiveMessage } from './market';

const ema = (windowStartMs: string, lastPrice = 10) => fromJson(EmaResultSchema, {
  symbol: 'PX1.FR', windowStartMs, lastPrice, ema38: lastPrice - 1, ema100: lastPrice - 2,
});
const advisory = (windowStartMs: string, warmup = false) => fromJson(AdvisorySchema, {
  symbol: 'PX1.FR', windowStartMs, side: 'SIDE_BUY', warmup,
});

describe('history and live merge', () => {
  it('deduplicates by message type and window while preserving a live update', () => {
    const current = applyLive(emptySnapshot(), { type: 'ema', data: ema('600000', 11) });
    const next = mergeHistory(current, { emaResults: [ema('300000'), ema('600000')], advisories: [advisory('300000', true)] }, [
      { type: 'ema', data: ema('600000', 12) },
      { type: 'advisory', data: advisory('600000') },
    ]);
    expect([...next.results.keys()]).toEqual([600000n, 300000n]);
    expect(next.results.get(600000n)?.lastPrice).toBe(12);
    expect(next.advisories.size).toBe(2);
    expect(next.advisories.get(300000n)?.warmup).toBe(true);
    expect(next.persistedThroughMs).toBe(600000n);
  });

  it('keeps the persisted cursor behind live points so later refreshes fill gaps', () => {
    const live = applyLive(emptySnapshot(), { type: 'ema', data: ema('900000') });
    const first = mergeHistory(live, { emaResults: [ema('300000')], advisories: [] }, []);
    expect(first.persistedThroughMs).toBe(300000n);
    const second = mergeHistory(first, { emaResults: [ema('600000'), ema('900000')], advisories: [] }, []);
    expect([...second.results.keys()].sort()).toEqual([300000n, 600000n, 900000n].sort());
  });

  it('decodes ProtoJSON int64 strings and enum names', () => {
    const message = parseLiveMessage({ type: 'advisory', data: {
      symbol: 'PX1.FR', windowStartMs: '1636387200000', side: 'SIDE_BUY', warmup: true,
    } });
    expect(message?.type).toBe('advisory');
    if (message?.type === 'advisory') {
      expect(message.data.windowStartMs).toBe(1636387200000n);
      expect(message.data.side).toBe(Side.BUY);
    }
  });
});
