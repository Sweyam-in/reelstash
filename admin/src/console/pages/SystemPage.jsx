import React from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@spatika/react';
import { RefreshCw } from 'lucide-react';
import { getHealth } from '../../api';
import { DetailList, ErrorState, MetricStrip, PageHeader, PageStack, Panel, PanelLink, SkeletonRows, Status } from '../components';
import { formatBytes, formatNumber, plural, timeAgo } from '../lib/format';
import { healthSummary } from '../lib/signals';
import { useResource } from '../lib/useResource';

const TONE = { ok: 'ok', warning: 'warn', error: 'crit' };
const CHECK_STATE = { ok: 'Healthy', warning: 'Warning', error: 'Problem' };
const CHECK_LINK = { providers: '/admin/metadata/providers', disks: '/admin/storage', tools: null, database: null };

function uptime(seconds) {
  const d = Math.floor(seconds / 86400); const h = Math.floor((seconds % 86400) / 3600); const m = Math.floor((seconds % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

// Refreshes every 30 seconds while the page is open. /admin/health is a plain read, so polling it is cheap.
export function SystemPage() {
  const health = useResource(() => getHealth(), [], { interval: 30_000 });
  const data = health.data;
  const summary = healthSummary(data);

  return <PageStack>
    <PageHeader title="System status" subtitle="Is ReelStash healthy, and if not, where to look. Refreshes every 30 seconds."
      actions={<Button variant="outline" disabled={health.refreshing} onClick={health.reload} leadingIcon={<RefreshCw size={14} className={health.refreshing ? 'console-spin' : ''} />}>Refresh</Button>} />
    {health.error && !data ? <Panel bodyless><ErrorState title="Couldn't load system status" error={health.error} onRetry={health.reload} /></Panel>
      : !data ? <Panel bodyless><SkeletonRows rows={6} label="Loading system status" /></Panel> : <>
        {health.error && <Panel bodyless><ErrorState compact title="The last refresh failed" error={health.error} onRetry={health.reload} /></Panel>}
        <div className="console-banner" data-tone={summary.tone === 'ok' ? undefined : summary.tone} role="status">
          <Status tone={summary.tone} pulse={summary.tone === 'ok'}><span className="console-banner__title">{summary.tone === 'ok' ? 'Everything is working' : summary.tone === 'crit' ? `Something needs attention: ${summary.label}` : `Working, with ${summary.label}`}</span></Status>
          <span className="console-banner__meta">Up {uptime(data.runtime.uptimeSeconds)} since {new Date(data.runtime.startedAt).toLocaleString()}</span>
        </div>
        <MetricStrip label="System figures" items={[
          { label: 'Database', value: `${data.database.latencyMs.toFixed(1)} ms`, detail: `${formatBytes(data.database.sizeBytes)} · ${data.database.poolInUse}/${data.database.poolMax} connections` },
          { label: 'Jobs', value: `${data.jobs.running} running`, detail: `${data.jobs.pending} queued · ${data.jobs.failed24h} failed in 24 h`, tone: data.jobs.failed24h ? 'warning' : undefined, to: '/admin/jobs' },
          { label: 'Streaming now', value: formatNumber(data.streams.streams), detail: `${data.streams.direct} direct · ${data.streams.hls} transcoding` },
          { label: 'Memory', value: formatBytes(data.runtime.heapBytes), detail: `${formatBytes(data.runtime.sysBytes)} reserved · ${data.runtime.goroutines.toLocaleString()} goroutines` },
        ]} />

        <Panel title="Checks" bodyless>
          <ul className="console-rows">{data.checks.map(check => <li key={check.id} className="console-row">
            <div className="console-row__main"><span className="console-row__title">{check.label}<Status tone={TONE[check.status]}>{CHECK_STATE[check.status] || check.status}</Status></span><span className="console-row__meta">{check.detail}</span></div>
            {check.status !== 'ok' && CHECK_LINK[check.id] && <div className="console-row__aside"><Button size="sm" variant="ghost" asChild><Link to={CHECK_LINK[check.id]}>Open</Link></Button></div>}
          </li>)}</ul>
        </Panel>

        <div className="console-grid-2">
          <Panel title="Database" bodyless><DetailList flush items={[
            { term: 'Response time', details: `${data.database.latencyMs.toFixed(1)} ms` }, { term: 'Size', details: formatBytes(data.database.sizeBytes) }, { term: 'PostgreSQL', details: data.database.version || '—' },
            { term: 'Connections', details: `${data.database.poolInUse} in use · ${data.database.poolIdle} idle · ${data.database.poolMax} max` },
          ]} /></Panel>
          <Panel title="Background jobs" actions={<PanelLink to="/admin/jobs">Open jobs</PanelLink>} bodyless>
            <DetailList flush items={[{ term: 'Running', details: data.jobs.running }, { term: 'Queued', details: data.jobs.pending }, { term: 'Oldest queued', details: data.jobs.oldestPendingAt ? timeAgo(data.jobs.oldestPendingAt) : '—' }, { term: 'Failed (24 h)', details: data.jobs.failed24h }]} />
            {!!data.jobs.recentFailures.length && <ul className="console-rows">{data.jobs.recentFailures.map(f => <li key={f.id} className="console-row"><div className="console-row__main"><span className="console-row__title"><Status tone="crit">{f.kind}</Status></span><span className="console-row__meta" title={f.error}>{f.error || 'Failed'}{f.finishedAt ? ` · ${timeAgo(f.finishedAt)}` : ''}</span></div><div className="console-row__aside"><Button size="sm" variant="ghost" asChild><Link to={`/admin/jobs?tab=history&job=${f.id}`}>Details</Link></Button></div></li>)}</ul>}
          </Panel>
          <Panel title="Metadata providers" actions={<PanelLink to="/admin/metadata/providers">Configure</PanelLink>} bodyless>
            <ul className="console-rows">{(data.providers || []).map(p => <li key={p.id} className="console-row"><div className="console-row__main"><span className="console-row__title">{p.name}</span></div><div className="console-row__aside"><Status tone={p.enabled ? 'ok' : 'neutral'}>{p.enabled ? 'Configured' : 'Not configured'}</Status></div></li>)}</ul>
          </Panel>
          <Panel title="Media tools" bodyless>
            <ul className="console-rows">{data.tools.map(t => <li key={t.name} className="console-row"><div className="console-row__main"><span className="console-row__title"><code className="console-code">{t.name}</code><Status tone={t.found ? 'ok' : 'crit'}>{t.found ? 'Found' : 'Missing'}</Status></span><span className="console-row__meta">{t.found ? `${t.version || 'version unknown'} · ${t.path}` : 'Not found on PATH. Playback, thumbnails and scans need it.'}</span></div></li>)}</ul>
          </Panel>
          <Panel title="Generated files" subtitle={data.caches.length ? `${formatBytes(data.caches.reduce((s, c) => s + c.bytes, 0))} in total` : 'Measuring… this can take a minute on a large library.'} actions={<PanelLink to="/admin/housekeeping">Housekeeping</PanelLink>} bodyless>
            {!!data.caches.length && <ul className="console-rows">{data.caches.map(c => <li key={c.name} className="console-row"><div className="console-row__main"><span className="console-row__title">{c.name}</span><span className="console-row__meta" title={c.path}>{c.error ? c.error : `${formatBytes(c.bytes)} · ${plural(c.files, 'file')}`}</span></div></li>)}</ul>}
          </Panel>
          <Panel title="Server" bodyless><DetailList flush items={[
            { term: 'Up for', details: `${uptime(data.runtime.uptimeSeconds)}` }, { term: 'Version', details: <>{data.runtime.version || 'dev'}{data.runtime.revision && <> <code className="console-code">{data.runtime.revision}</code></>}</> }, { term: 'Go', details: data.runtime.goVersion },
            { term: 'Memory', details: `${formatBytes(data.runtime.heapBytes)} heap · ${formatBytes(data.runtime.sysBytes)} reserved` }, { term: 'CPUs', details: data.runtime.cpus }, { term: 'Goroutines', details: data.runtime.goroutines.toLocaleString() },
          ]} /></Panel>
          <Panel title="About ReelStash" bodyless><DetailList flush items={[
            { term: 'Version', details: data.runtime.version || 'dev' },
            { term: 'Made by', details: <a className="console-link" href="https://sweyam.com" target="_blank" rel="noopener noreferrer">Sweyam</a> },
            { term: 'Product page', details: <a className="console-link" href="https://sweyam.com/products/reelstash" target="_blank" rel="noopener noreferrer">sweyam.com/products/reelstash</a> },
            { term: 'Source and releases', details: <a className="console-link" href="https://github.com/Sweyam-in/reelstash" target="_blank" rel="noopener noreferrer">github.com/Sweyam-in/reelstash</a> },
            { term: 'License', details: 'GNU GPL v3' },
          ]} /></Panel>
        </div>
      </>}
  </PageStack>;
}
