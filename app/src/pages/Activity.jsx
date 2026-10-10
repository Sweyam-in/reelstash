import React, { useEffect, useMemo, useRef, useState } from 'react';
import { monogram } from '../components/Monogram';
import { Link } from 'react-router-dom';
import { Clock3, Film, Flame, Play, Sparkles, Timer } from 'lucide-react';
import { artworkUrl, getWatchActivity, performerImageUrl } from '../api';
import { timeAgo } from './timeUtil';
import useRefetchOnFocus from '../hooks/useRefetchOnFocus';
import { useErrorToast } from '../hooks/useErrorToast';

const RANGES = [[7, '7 days'], [30, '30 days'], [90, '90 days'], [365, 'Year']];

function duration(seconds) {
  if (!seconds || seconds < 60) return `${Math.round(seconds || 0)}s`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return h ? `${h}h ${m}m` : `${m}m`;
}

const prefersReducedMotion = () => typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;

// Counts up from 0 on mount / when the value changes, so numbers feel earned.
function useCountUp(target, ms = 900) {
  const [value, setValue] = useState(prefersReducedMotion() ? target : 0);
  useEffect(() => {
    if (prefersReducedMotion() || !target) { setValue(target); return undefined; }
    let raf; const start = performance.now();
    const tick = now => {
      const t = Math.min(1, (now - start) / ms);
      setValue(target * (1 - Math.pow(1 - t, 3)));
      if (t < 1) raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [target, ms]);
  return value;
}

function Sparkline({ values }) {
  if (values.length < 2) return null;
  const max = Math.max(1, ...values);
  const pts = values.map((v, i) => [(i / (values.length - 1)) * 100, 100 - (v / max) * 86 - 7]);
  const line = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ');
  return <svg className="rs-spark" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
    <path className="rs-spark-area" d={`${line} L100,100 L0,100 Z`} />
    <path className="rs-spark-line" d={line} pathLength="1" vectorEffect="non-scaling-stroke" />
  </svg>;
}

function StatTile({ hue, icon: Icon, label, value, format = n => Math.round(n).toLocaleString(), suffix, foot, spark, ring, delay = 0 }) {
  const shown = useCountUp(value);
  return <div className="rs-stat" style={{ '--h': hue, '--d': `${delay}ms` }}>
    <div className="rs-stat-top">
      <span className="rs-stat-icon"><Icon size={18} /></span>
      <span className="rs-stat-label">{label}</span>
      {ring != null && <svg className="rs-stat-ring" viewBox="0 0 36 36" aria-hidden="true"><circle cx="18" cy="18" r="15" /><circle className="rs-stat-ring-fill" cx="18" cy="18" r="15" pathLength="100" style={{ '--pct': Math.max(0, Math.min(100, ring)) }} /></svg>}
    </div>
    <strong className="rs-stat-value">{format(shown)}{suffix && <small>{suffix}</small>}</strong>
    {foot && <span className="rs-stat-foot">{foot}</span>}
    {spark && <Sparkline values={spark} />}
  </div>;
}

function fmtDay(date) {
  const d = new Date(`${date}T00:00:00`);
  return Number.isNaN(d.getTime()) ? date : d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' });
}

// Smooth area chart with a hover read-out; the line draws itself in on load.
function TrendChart({ data }) {
  const [hover, setHover] = useState(null);
  const ref = useRef(null);
  const max = Math.max(1, ...data.map(d => d.plays));
  const geo = useMemo(() => data.map((d, i) => ({
    x: data.length > 1 ? (i / (data.length - 1)) * 100 : 50,
    y: 100 - (d.plays / max) * 84 - 8,
  })), [data, max]);
  const curve = useMemo(() => geo.reduce((acc, p, i) => {
    if (!i) return `M${p.x},${p.y}`;
    const q = geo[i - 1]; const cx = (q.x + p.x) / 2;
    return `${acc} C${cx},${q.y} ${cx},${p.y} ${p.x},${p.y}`;
  }, ''), [geo]);
  const hasPlays = data.some(d => d.plays > 0);
  const peak = data.reduce((best, d, i) => (d.plays > (data[best]?.plays ?? -1) ? i : best), 0);
  const active = hover ?? (data[peak]?.plays ? peak : null);
  const move = e => {
    const box = ref.current.getBoundingClientRect();
    const f = Math.min(1, Math.max(0, (e.clientX - box.left) / box.width));
    setHover(Math.round(f * (data.length - 1)));
  };
  if (!hasPlays) return <p className="rs-trend-empty">No plays in this range yet. Watch something and your trend shows up here.</p>;
  return <div className="rs-trend" ref={ref} onPointerMove={move} onPointerLeave={() => setHover(null)}>
    <svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="Plays per day">
      <defs><linearGradient id="rs-trend-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="currentColor" stopOpacity=".38" /><stop offset="1" stopColor="currentColor" stopOpacity="0" /></linearGradient></defs>
      {[25, 50, 75].map(y => <line key={y} className="rs-trend-grid" x1="0" x2="100" y1={y} y2={y} vectorEffect="non-scaling-stroke" />)}
      <path className="rs-trend-area" d={`${curve} L100,100 L0,100 Z`} fill="url(#rs-trend-fill)" />
      <path className="rs-trend-line" d={curve} pathLength="1" vectorEffect="non-scaling-stroke" />
    </svg>
    {active != null && data[active] && <>
      <span className="rs-trend-cursor" style={{ left: `${geo[active].x}%` }} />
      <span className="rs-trend-dot" style={{ left: `${geo[active].x}%`, top: `${geo[active].y}%` }} />
      <div className={`rs-trend-tip${geo[active].x > 78 ? ' is-right' : ''}`} style={{ left: `${geo[active].x}%` }}>
        <b>{data[active].plays} {data[active].plays === 1 ? 'play' : 'plays'}</b><span>{fmtDay(data[active].date)}{data[active].watchSeconds ? ` · ${duration(data[active].watchSeconds)}` : ''}</span>
      </div>
    </>}
    <div className="rs-trend-axis"><span>{data[0] && fmtDay(data[0].date)}</span><span>{data.length && fmtDay(data[data.length - 1].date)}</span></div>
  </div>;
}

function hourLabel(h) { return `${h % 12 || 12}${h < 12 ? 'am' : 'pm'}`; }

function HourChart({ counts }) {
  const max = Math.max(1, ...counts);
  const peak = counts.indexOf(Math.max(...counts));
  return <>
    <div className="rs-hours" role="img" aria-label="Plays by hour of day">
      {counts.map((count, hour) => <span key={hour} title={`${hourLabel(hour)} — ${count}`} className={count && hour === peak ? 'is-peak' : count ? 'has' : ''} style={{ '--v': Math.max(0.05, count / max), '--i': hour }} />)}
    </div>
    <small className="rs-act-axis"><span>12am</span><span>6am</span><span>12pm</span><span>6pm</span><span>11pm</span></small>
    <p className="rs-hours-note">{Math.max(...counts) ? <>Most active around <b>{hourLabel(peak)}</b></> : 'No plays in this range yet.'}</p>
  </>;
}

function cheer(t, days) {
  if (!t.plays) return 'Nothing played yet. Start something and this page comes alive.';
  const rate = t.plays ? Math.round((t.completions / t.plays) * 100) : 0;
  if (t.completions >= 5) return `${t.completions} titles finished in ${days === 365 ? 'the last year' : `${days} days`}. That’s a great run!`;
  if (rate >= 50) return `You finish what you start — ${rate}% of plays went all the way.`;
  if (t.uniqueTitles >= 10) return `${t.uniqueTitles} different titles explored. Plenty of variety!`;
  return `${t.plays} ${t.plays === 1 ? 'play' : 'plays'} so far. Nice start!`;
}

function Thumb({ item }) {
  const [ok, setOk] = useState(true);
  return <Link to={`/library/${item.id}`} className="rs-act-thumb" tabIndex={-1} aria-hidden="true">
    {ok ? <img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={() => setOk(false)} /> : <Film size={20} />}
  </Link>;
}

function MediaList({ items, meta, badge }) {
  return <ol className="rs-act-media">{items.map((item, i) => <li key={`${item.id}-${i}`}>
    <Thumb item={item} />
    <div className="rs-act-media-body"><Link to={`/library/${item.id}`}>{item.title}</Link><small>{meta(item)}</small></div>
    <div className="rs-act-media-side">{badge(item)}</div>
  </li>)}</ol>;
}

function Ranking({ title, rows, unit, link, image }) {
  if (!rows?.length) return null;
  const max = Math.max(1, ...rows.map(r => r.plays));
  return <section className="rs-act-card">
    <h2>{title}</h2>
    <ol>{rows.map(row => <li key={row.id}>
      {image && <span className={`rs-act-avatar${row.profileImageId ? '' : ' rs-mono'}`} style={row.profileImageId ? undefined : monogram(row.name).style}>{row.profileImageId ? <img src={performerImageUrl(row.id)} alt="" loading="lazy" /> : monogram(row.name).initials}</span>}
      <div><Link to={link(row)}>{row.name}</Link><small>{row.plays} {row.plays === 1 ? 'play' : 'plays'} · {duration(row.watchSeconds)} · {row.titles} {row.titles === 1 ? 'title' : 'titles'}</small><i style={{ width: `${(row.plays / max) * 100}%` }} /></div>
    </li>)}</ol>
  </section>;
}

export default function Activity() {
  const [days, setDays] = useState(30);
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const notifyError = useErrorToast();
  const load = React.useCallback(() => {
    setLoading(true);
    getWatchActivity(days).then(setData).catch(err => notifyError(err, 'Could not load activity', { id: 'activity-load' })).finally(() => setLoading(false));
  }, [days]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(load, [load]);
  useRefetchOnFocus(load);

  const t = data?.totals;
  const dayPlays = data?.byDay.map(d => d.plays) || [];
  const dayWatch = data?.byDay.map(d => d.watchSeconds) || [];
  return <div className="rs-page rs-activity">
    <header className="rs-page-header rs-act-header">
      <div className="rs-act-headrow">
        <div className="rs-act-title"><p className="rs-eyebrow">Activity</p><h1>Your watching</h1><p>Plays, watch time, and what you come back to most.</p></div>
        <div className="rs-act-controls">
          <div className="rs-act-ranges" role="group" aria-label="Time range">{RANGES.map(([d, label]) => <button key={d} type="button" className={d === days ? 'is-active' : ''} aria-pressed={d === days} onClick={() => setDays(d)}>{label}</button>)}</div>
        </div>
      </div>
    </header>
    {data && <div key={days}>
      <p className="rs-cheer"><Sparkles size={16} /> {cheer(t, days)}</p>
      <div className="rs-stats">
        <StatTile hue={262} icon={Play} label="Plays" value={t.plays} spark={dayPlays} delay={0} />
        <StatTile hue={178} icon={Clock3} label="Watch time" value={t.watchSeconds} format={duration} spark={dayWatch} delay={70} />
        <StatTile hue={38} icon={Film} label="Unique titles" value={t.uniqueTitles} suffix={` / ${t.libraryTitles.toLocaleString()}`} ring={t.libraryTitles ? (t.uniqueTitles / t.libraryTitles) * 100 : 0} foot={t.libraryTitles ? `${Math.max(1, Math.round((t.uniqueTitles / t.libraryTitles) * 100))}% of your library` : undefined} delay={140} />
        <StatTile hue={145} icon={Flame} label="Completed" value={t.completions} ring={t.plays ? (t.completions / t.plays) * 100 : 0} foot={t.plays ? `${Math.round((t.completions / t.plays) * 100)}% of plays finished` : undefined} delay={210} />
        <StatTile hue={335} icon={Timer} label="Avg. session" value={t.avgSessionSeconds} format={duration} foot="per sitting" delay={280} />
      </div>
      <div className="rs-act-grid">
        <section className="rs-act-card rs-act-wide"><h2>Plays per day</h2><TrendChart data={data.byDay} /></section>
        <section className="rs-act-card rs-act-wide"><h2>Time of day</h2><HourChart counts={data.byHour} /></section>
        <Ranking title="Most played performers" rows={data.performers} link={row => `/performers/${row.id}`} image />
        <Ranking title="Top studios" rows={data.studios} link={row => `/studios/${row.id}`} />
        <Ranking title="Top categories" rows={data.categories} link={row => `/categories/${row.id}`} />
        <section className="rs-act-card rs-act-wide"><h2>Most played</h2>
          {data.mostPlayed.length ? <MediaList items={data.mostPlayed}
            meta={item => [item.studioName, (item.performers || []).slice(0, 2).join(', ')].filter(Boolean).join(' · ')}
            badge={item => <><b>{item.plays}×</b><em>{duration(item.watchSeconds)}</em></>} /> : <p className="rs-act-empty">Nothing played in this range yet.</p>}
        </section>
        <section className="rs-act-card rs-act-wide"><h2>Recent history</h2>
          {data.recent.length ? <MediaList items={data.recent.slice(0, 10)}
            meta={item => [timeAgo(item.lastPlayedAt), item.studioName].filter(Boolean).join(' · ')}
            badge={item => <><b className={item.completed ? 'is-done' : ''}>{item.completed ? 'Finished' : 'Partial'}</b><em>{duration(item.watchSeconds)}</em></>} /> : <p className="rs-act-empty">No history yet. Play something and it will show up here.</p>}
        </section>
      </div>
    </div>}
  </div>;
}
