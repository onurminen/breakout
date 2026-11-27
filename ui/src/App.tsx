import { useEffect, useMemo, useRef, useState } from 'react';
import {
  createChart,
  createSeriesMarkers,
  LineSeries,
  TickMarkType,
  type IChartApi,
  type ISeriesApi,
  type ISeriesMarkersPluginApi,
  type SeriesMarker,
  type Time,
  type UTCTimestamp,
} from 'lightweight-charts';
import { Side, type Advisory, type EmaResult } from './gen/breakout/results/v1/results_pb';
import { useMarketData, type Latest } from './useMarketData';
import type { SymbolSnapshot } from './market';
import './styles.css';

type SymbolOption = { symbol: string; secType: string };
type Kpis = {
  eventsPerSecond: number | null;
  eventLatencyP95Seconds: number | null;
  windowResultLatencyP95Seconds: number | null;
  lateEvents: number | null;
};

const dateTime = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'UTC', day: '2-digit', month: 'short', year: 'numeric',
  hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
});
const axisTime = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
const axisDay = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit', month: 'short' });
const number = new Intl.NumberFormat('en-US', { maximumFractionDigits: 2 });
const precise = new Intl.NumberFormat('en-US', { minimumFractionDigits: 4, maximumFractionDigits: 4 });
const shortTime = new Intl.DateTimeFormat('en-GB', { timeZone: 'UTC', day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
const windowMs = 5n * 60n * 1000n;

function windowLabel(startMs: bigint): string {
  return `${shortTime.format(new Date(Number(startMs)))}–${axisTime.format(new Date(Number(startMs + windowMs)))}`;
}

function utc(ms: bigint): string {
  return `${dateTime.format(new Date(Number(ms)))} UTC`;
}

function chartTime(time: Time): string {
  return utc(BigInt(Number(time) * 1000));
}

function tickTime(time: Time, type: TickMarkType): string {
  const date = new Date(Number(time) * 1000);
  return type === TickMarkType.Time || type === TickMarkType.TimeWithSeconds ? axisTime.format(date) : axisDay.format(date);
}

function timestamp(ms: bigint): UTCTimestamp {
  return (Number(ms) / 1000) as UTCTimestamp;
}

function SymbolPicker({ symbols, selected, onToggle, onSelectMany, onClear, error }: {
  symbols: SymbolOption[];
  selected: string[];
  onToggle: (symbol: string) => void;
  onSelectMany: (symbols: string[]) => void;
  onClear: () => void;
  error: string | null;
}) {
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<'all' | 'equity' | 'index'>('all');
  const matching = symbols.filter(({ symbol, secType }) =>
    symbol.toLowerCase().includes(query.trim().toLowerCase()) &&
    (filter === 'all' || secType === `SEC_TYPE_${filter.toUpperCase()}`));
  const visible = matching.slice(0, 100);
  const chosen = new Set(selected);

  return <section className="panel picker" aria-labelledby="symbols-heading">
    <div className="panel-heading">
      <div><p className="eyebrow">Watchlist</p><h2 id="symbols-heading">Symbols</h2></div>
      <span className="count">{number.format(selected.length)} selected</span>
    </div>
    <label className="search-label" htmlFor="symbol-search">Search symbols</label>
    <input id="symbol-search" className="search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search by ticker…" />
    <div className="filters" aria-label="Security type">
      {(['all', 'equity', 'index'] as const).map((option) =>
        <button key={option} type="button" className={filter === option ? 'filter active' : 'filter'} onClick={() => setFilter(option)} aria-pressed={filter === option}>
          {option === 'all' ? 'All' : option === 'equity' ? 'Equities' : 'Indices'}
        </button>)}
    </div>
    <div className="picker-actions">
      <button type="button" className="filter" disabled={matching.length === 0} onClick={() => onSelectMany(matching.map(({ symbol }) => symbol))}>Select all {matching.length ? number.format(matching.length) : ''}</button>
      <button type="button" className="filter" disabled={selected.length === 0} onClick={onClear}>Deselect all</button>
    </div>
    {error && <p className="notice error" role="alert">{error}</p>}
    <div className="symbol-list">
      {!error && symbols.length === 0 && <p className="empty-small">No symbols stored yet. This list updates during a replay.</p>}
      {symbols.length > 0 && matching.length === 0 && <p className="empty-small">No matching symbols.</p>}
      {visible.map(({ symbol, secType }) => <label className="symbol-row" key={symbol}>
        <input type="checkbox" checked={chosen.has(symbol)} onChange={() => onToggle(symbol)} />
        <span className="ticker">{symbol}</span>
        <span className="symbol-type">{secType === 'SEC_TYPE_INDEX' ? 'Index' : 'Equity'}</span>
      </label>)}
      {matching.length > 100 && <p className="list-hint">Showing 100 of {matching.length}. Search to narrow the list.</p>}
    </div>
  </section>;
}

function KpiCards({ kpis, error }: { kpis: Kpis | null; error: string | null }) {
  const cards = [
    { label: 'Events / second', value: kpis?.eventsPerSecond == null ? '—' : number.format(kpis.eventsPerSecond), unit: 'events/s' },
    { label: 'Event latency · p95', value: kpis?.eventLatencyP95Seconds == null ? '—' : number.format(kpis.eventLatencyP95Seconds * 1000), unit: 'ms' },
    { label: 'Result latency · p95', value: kpis?.windowResultLatencyP95Seconds == null ? '—' : number.format(kpis.windowResultLatencyP95Seconds * 1000), unit: 'ms' },
    { label: 'Late events', value: kpis?.lateEvents == null ? '—' : number.format(kpis.lateEvents), unit: 'total' },
  ];
  return <section className="kpi-section" aria-label="System performance">
    <div className="section-title"><div><p className="eyebrow">Pipeline health</p><h2>Performance</h2></div>{error && <span className="quiet-error" role="status">KPIs unavailable</span>}</div>
    <div className="kpi-grid">{cards.map((card) => <div className="panel kpi" key={card.label}>
      <span className="kpi-label">{card.label}</span><span className="kpi-value">{card.value}</span><span className="kpi-unit">{card.unit}</span>
    </div>)}</div>
  </section>;
}

function SymbolChart({ symbol, snapshot }: { symbol: string; snapshot: SymbolSnapshot }) {
  const container = useRef<HTMLDivElement>(null);
  const chart = useRef<IChartApi | null>(null);
  const series = useRef<{
    price: ISeriesApi<'Line'>;
    ema38: ISeriesApi<'Line'>;
    ema100: ISeriesApi<'Line'>;
    markers: ISeriesMarkersPluginApi<Time>;
  } | null>(null);
  const fitted = useRef(false);

  useEffect(() => {
    if (!container.current) return;
    const instance = createChart(container.current, {
      autoSize: true,
      layout: { background: { color: '#172337' }, textColor: '#aebed1', attributionLogo: true },
      grid: { vertLines: { color: '#243349' }, horzLines: { color: '#243349' } },
      rightPriceScale: { borderColor: '#34465d' },
      timeScale: { borderColor: '#34465d', timeVisible: true, secondsVisible: false, tickMarkFormatter: tickTime },
      localization: { timeFormatter: chartTime },
      crosshair: { vertLine: { labelBackgroundColor: '#385477' }, horzLine: { labelBackgroundColor: '#385477' } },
    });
    const price = instance.addSeries(LineSeries, { color: '#76d6ed', lineWidth: 2, title: 'Last price' });
    const ema38 = instance.addSeries(LineSeries, { color: '#f6b866', lineWidth: 2, title: 'EMA38' });
    const ema100 = instance.addSeries(LineSeries, { color: '#b49bf5', lineWidth: 2, title: 'EMA100' });
    chart.current = instance;
    series.current = { price, ema38, ema100, markers: createSeriesMarkers(price, []) };
    return () => { instance.remove(); chart.current = null; series.current = null; fitted.current = false; };
  }, []);

  useEffect(() => {
    if (!series.current) return;
    const results = [...snapshot.results.values()].sort((a, b) => Number(a.windowStartMs - b.windowStartMs));
    series.current.price.setData(results.map((item) => ({ time: timestamp(item.windowStartMs), value: item.lastPrice })));
    series.current.ema38.setData(results.map((item) => ({ time: timestamp(item.windowStartMs), value: item.ema38 })));
    series.current.ema100.setData(results.map((item) => ({ time: timestamp(item.windowStartMs), value: item.ema100 })));
    const markers: SeriesMarker<Time>[] = [...snapshot.advisories.values()]
      .filter((advisory) => snapshot.results.has(advisory.windowStartMs))
      .sort((a, b) => Number(a.windowStartMs - b.windowStartMs))
      .map((advisory) => ({
        time: timestamp(advisory.windowStartMs),
        position: advisory.side === Side.BUY ? 'belowBar' : 'aboveBar',
        shape: advisory.side === Side.BUY ? 'arrowUp' : 'arrowDown',
        color: advisory.side === Side.BUY ? '#57d4a3' : '#ff8c89',
        text: advisory.warmup ? 'Warm-up' : advisory.side === Side.BUY ? 'Buy' : 'Sell',
      }));
    series.current.markers.setMarkers(markers);
    if (results.length && !fitted.current) { chart.current?.timeScale().fitContent(); fitted.current = true; }
  }, [snapshot]);

  let latest: EmaResult | undefined;
  for (const result of snapshot.results.values()) {
    if (!latest || result.windowStartMs > latest.windowStartMs) latest = result;
  }
  return <article className="panel chart-card" aria-label={`${symbol} price and EMA chart`}>
    <div className="chart-header">
      <div><p className="eyebrow">Price & moving averages</p><h3>{symbol}</h3></div>
      <div className="last-price"><span>Last price</span><strong>{latest ? number.format(latest.lastPrice) : '—'}</strong></div>
    </div>
    <div className="legend" aria-hidden="true"><span className="legend-price">Last price</span><span className="legend-ema38">EMA38</span><span className="legend-ema100">EMA100</span></div>
    <div className="chart-box" ref={container} />
    {snapshot.loading && snapshot.results.size === 0 && <p className="chart-caption">Loading history…</p>}
    {!snapshot.loading && snapshot.results.size === 0 && !snapshot.error && <p className="chart-caption">No windows for this symbol yet.</p>}
    {snapshot.error && <p className="chart-caption error" role="status">{snapshot.error}. Retrying automatically.</p>}
    {latest && <p className="chart-caption">Latest window: {utc(latest.windowStartMs)}</p>}
  </article>;
}

function Pager({ page, pages, onPage }: { page: number; pages: number; onPage: (page: number) => void }) {
  if (pages <= 1) return null;
  return <div className="pager">
    <button type="button" className="filter" disabled={page === 0} onClick={() => onPage(page - 1)}>Previous</button>
    <span>Page {page + 1} of {pages}</span>
    <button type="button" className="filter" disabled={page >= pages - 1} onClick={() => onPage(page + 1)}>Next</button>
  </div>;
}

function usePaged<T>(items: T[], size: number) {
  const [page, setPage] = useState(0);
  const pages = Math.max(1, Math.ceil(items.length / size));
  const current = Math.min(page, pages - 1);
  return { rows: items.slice(current * size, (current + 1) * size), pager: <Pager page={current} pages={pages} onPage={setPage} /> };
}

function Signal({ advisory }: { advisory?: Advisory }) {
  if (!advisory) return <span className="muted">—</span>;
  return <span className="signal-cell">
    <span className={advisory.side === Side.BUY ? 'signal buy' : 'signal sell'}>{advisory.side === Side.BUY ? 'Buy ↑' : 'Sell ↓'}</span>
    {advisory.warmup && <span className="warmup" title="The first window starts the EMA from zero">Warm-up</span>}
  </span>;
}

function Trend({ result }: { result?: EmaResult }) {
  if (!result) return <span className="muted">—</span>;
  return result.ema38 > result.ema100 ? <span className="trend up">▲ EMA38 above</span> : <span className="trend down">▼ EMA38 below</span>;
}

function OverviewTable({ selected, latest, focused, onFocus }: {
  selected: string[];
  latest: Record<string, Latest>;
  focused: string | null;
  onFocus: (symbol: string) => void;
}) {
  const sorted = useMemo(() => [...selected].sort(), [selected]);
  const { rows, pager } = usePaged(sorted, 20);
  return <section className="panel table-panel" aria-labelledby="overview-heading">
    <div className="panel-heading"><div><p className="eyebrow">Selected symbols · live</p><h2 id="overview-heading">Overview</h2></div><span className="count">Select a row for its chart and windows</span></div>
    <div className="table-scroll"><table className="data-table">
      <thead><tr><th>Symbol</th><th>Latest window</th><th className="num">Last price</th><th className="num">EMA38</th><th className="num">EMA100</th><th>Trend</th><th>Last advisory</th></tr></thead>
      <tbody>{rows.map((symbol) => {
        const { result, advisory } = latest[symbol] ?? {};
        return <tr key={symbol} className={symbol === focused ? 'focused' : undefined} onClick={() => onFocus(symbol)}>
          <td><button type="button" className="row-link" aria-pressed={symbol === focused}>{symbol}</button></td>
          <td>{result ? windowLabel(result.windowStartMs) : '—'}</td>
          <td className="num">{result ? precise.format(result.lastPrice) : '—'}</td>
          <td className="num">{result ? precise.format(result.ema38) : '—'}</td>
          <td className="num">{result ? precise.format(result.ema100) : '—'}</td>
          <td><Trend result={result} /></td>
          <td><Signal advisory={advisory} />{advisory && <span className="muted"> {shortTime.format(new Date(Number(advisory.windowStartMs)))}</span>}</td>
        </tr>;
      })}</tbody>
    </table></div>
    {pager}
    <p className="list-hint">Rows fill in as windows close during the replay.</p>
  </section>;
}

function WindowTable({ symbol, snapshot }: { symbol: string; snapshot: SymbolSnapshot }) {
  const [onlyAdvisories, setOnlyAdvisories] = useState(false);
  const windows = useMemo(() => [...snapshot.results.values()]
    .filter((result) => !onlyAdvisories || snapshot.advisories.has(result.windowStartMs))
    .sort((a, b) => Number(b.windowStartMs - a.windowStartMs)), [snapshot, onlyAdvisories]);
  const { rows, pager } = usePaged(windows, 25);
  return <section className="panel table-panel" aria-labelledby="windows-heading">
    <div className="panel-heading"><div><p className="eyebrow">Every window of {symbol}</p><h2 id="windows-heading">Windows</h2></div><span className="count">{number.format(windows.length)} windows</span></div>
    <label className="check"><input type="checkbox" checked={onlyAdvisories} onChange={(event) => setOnlyAdvisories(event.target.checked)} /> Only windows with an advisory</label>
    <div className="table-scroll"><table className="data-table">
      <thead><tr><th>Window</th><th className="num">Last price</th><th className="num">EMA38</th><th className="num">EMA100</th><th>Advisory</th></tr></thead>
      <tbody>{rows.map((result) => <tr key={String(result.windowStartMs)}>
        <td>{windowLabel(result.windowStartMs)}</td>
        <td className="num">{precise.format(result.lastPrice)}</td>
        <td className="num">{precise.format(result.ema38)}</td>
        <td className="num">{precise.format(result.ema100)}</td>
        <td><Signal advisory={snapshot.advisories.get(result.windowStartMs)} /></td>
      </tr>)}</tbody>
    </table></div>
    {windows.length === 0 && <p className="empty-small">{snapshot.loading ? 'Loading history…' : 'No windows yet.'}</p>}
    {pager}
  </section>;
}

function AdvisoryTable({ advisories, onFocus }: { advisories: Advisory[]; onFocus: (symbol: string) => void }) {
  const sorted = useMemo(() => [...advisories].sort((a, b) =>
    Number(b.windowStartMs - a.windowStartMs) || a.symbol.localeCompare(b.symbol)), [advisories]);
  const { rows, pager } = usePaged(sorted, 50);
  return <section className="panel table-panel" aria-labelledby="advisories-heading">
    <div className="panel-heading"><div><p className="eyebrow">Selected symbols · live</p><h2 id="advisories-heading">Advisories</h2></div><span className="count">{number.format(sorted.length)} received</span></div>
    <div className="table-scroll"><table className="data-table">
      <thead><tr><th>Window</th><th>Symbol</th><th>Advisory</th><th className="num">EMA38</th><th className="num">EMA100</th></tr></thead>
      <tbody>{rows.map((advisory) => <tr key={`${advisory.symbol}:${advisory.windowStartMs}`} onClick={() => onFocus(advisory.symbol)}>
        <td>{windowLabel(advisory.windowStartMs)}</td>
        <td><button type="button" className="row-link">{advisory.symbol}</button></td>
        <td><Signal advisory={advisory} /></td>
        <td className="num">{precise.format(advisory.ema38)}</td>
        <td className="num">{precise.format(advisory.ema100)}</td>
      </tr>)}</tbody>
    </table></div>
    {sorted.length === 0 && <p className="empty-small">No advisories since the symbols were selected.</p>}
    {pager}
    <p className="list-hint">Advisories that arrive live while the symbols are selected. A symbol's full history is in its Windows table.</p>
  </section>;
}

export default function App() {
  const [symbols, setSymbols] = useState<SymbolOption[]>([]);
  const [symbolsError, setSymbolsError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string[]>(() => {
    try {
      const saved: unknown = JSON.parse(localStorage.getItem('breakout:selected') ?? '[]');
      return Array.isArray(saved) ? saved.filter((value): value is string => typeof value === 'string') : [];
    } catch { return []; }
  });
  const [focused, setFocused] = useState<string | null>(null);
  const [tab, setTab] = useState<'overview' | 'advisories'>('overview');
  const [kpis, setKpis] = useState<Kpis | null>(null);
  const [kpiError, setKpiError] = useState<string | null>(null);
  const shown = focused !== null && selected.includes(focused) ? focused : [...selected].sort()[0] ?? null;
  const { snapshot, latest, advisories, status } = useMarketData(selected, shown);

  useEffect(() => {
    try { localStorage.setItem('breakout:selected', JSON.stringify(selected)); } catch { /* Browsing still works without storage. */ }
  }, [selected]);

  useEffect(() => {
    let active = true;
    async function loadSymbols() {
      try {
        const response = await fetch('/api/symbols');
        if (!response.ok) throw new Error(`Symbol request failed (${response.status})`);
        const body: unknown = await response.json();
        if (!Array.isArray(body)) throw new Error('Invalid symbol response');
        if (active) { setSymbols(body as SymbolOption[]); setSymbolsError(null); }
      } catch { if (active) setSymbolsError('Symbols are unavailable. Retrying automatically.'); }
    }
    void loadSymbols();
    const timer = setInterval(() => void loadSymbols(), 10_000);
    return () => { active = false; clearInterval(timer); };
  }, []);

  useEffect(() => {
    let active = true;
    async function loadKpis() {
      try {
        const response = await fetch('/api/kpis');
        if (!response.ok) throw new Error(`KPI request failed (${response.status})`);
        const body = await response.json() as Kpis;
        if (active) { setKpis(body); setKpiError(null); }
      } catch { if (active) { setKpis(null); setKpiError('KPIs unavailable'); } }
    }
    void loadKpis();
    const timer = setInterval(() => void loadKpis(), 5_000);
    return () => { active = false; clearInterval(timer); };
  }, []);

  function toggle(symbol: string) {
    setSelected((current) => current.includes(symbol) ? current.filter((item) => item !== symbol) : [...current, symbol]);
  }

  function focus(symbol: string) {
    setFocused(symbol);
    setTab('overview');
  }

  return <div className="app-shell">
    <header className="topbar"><div className="brand"><span className="brand-mark">↗</span><div><span className="brand-name">breakout</span><span className="brand-subtitle">Live market signals</span></div></div><div className={`connection ${status}`} role="status"><span className="status-dot" />{status === 'connected' ? 'Live connection' : status === 'connecting' ? 'Connecting…' : 'Reconnecting…'}</div></header>
    <main>
      <div className="intro"><div><p className="eyebrow">Streaming analytics</p><h1>Market overview</h1><p>Price, moving averages, and crossover signals as the replay runs.</p></div><span className="utc-badge">All times UTC</span></div>
      <KpiCards kpis={kpis} error={kpiError} />
      <div className="workspace"><SymbolPicker symbols={symbols} selected={selected} onToggle={toggle}
        onSelectMany={(names) => setSelected((current) => [...new Set([...current, ...names])])} onClear={() => setSelected([])} error={symbolsError} />
        <section className="charts" aria-label="Results">
          {selected.length === 0 ? <div className="panel empty-selection"><span className="empty-icon">⌁</span><h2>Choose symbols</h2><p>Search the watchlist, or select all matching symbols, to follow their EMAs and advisories.</p></div> : <>
            <div className="tabs" role="tablist">
              {(['overview', 'advisories'] as const).map((name) => <button key={name} type="button" role="tab" aria-selected={tab === name} className={tab === name ? 'tab active' : 'tab'} onClick={() => setTab(name)}>
                {name === 'overview' ? 'Overview' : `Advisories (${number.format(advisories.length)})`}
              </button>)}
            </div>
            {tab === 'advisories' ? <AdvisoryTable advisories={advisories} onFocus={focus} /> : <>
              <OverviewTable selected={selected} latest={latest} focused={shown} onFocus={setFocused} />
              {shown && snapshot && <SymbolChart key={shown} symbol={shown} snapshot={snapshot} />}
              {shown && snapshot && <WindowTable key={`windows:${shown}`} symbol={shown} snapshot={snapshot} />}
            </>}
          </>}
        </section>
      </div>
    </main>
    <footer>Market data from the DEBS 2022 Grand Challenge · Charts by <a href="https://www.tradingview.com/" target="_blank" rel="noreferrer">TradingView</a></footer>
  </div>;
}
