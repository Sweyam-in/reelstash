import React, { useMemo, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Button } from '@spatika/react';
import {
  AlertTriangle, ArrowRight, CheckCircle2, ClipboardCheck, Copy, Database, HardDrive, Plug, RefreshCw, Rocket, Sparkles, WifiOff, Wrench,
} from 'lucide-react';
import { cancelJob, getAnalytics, getLibraries, getOverview, getStorageLocations, getUsageAnalytics, startLibraryScan } from '../../api';
import { isActive, jobIcon, jobName, jobProgress, resultSummary } from '../../pages/jobKinds';
import { ConsoleBarChart, EmptyState, ErrorState, JobStatus, MetricStrip, PageHeader, PageStack, Panel, PanelLink, ProgressBar, RangeTabs, SkeletonRows, Status, Meter } from '../components';
import { formatBytes, formatHours, formatNumber, isLowSpace, plural, timeAgo, usedFraction } from '../lib/format';
import { useJobs } from '../lib/useJobs';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useSignals } from '../lib/signals';

const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;
const RANGES = [{ value: '7', label: '7d' }, { value: '30', label: '30d' }, { value: '90', label: '90d' }];
const TONE_RANK = { crit: 0, warn: 1, info: 2 };

// What needs a human, most urgent first. Every row comes from a real signal; empty when all is well.
function attentionItems({ overview, disks, reviewCount, health }) {
  const items = [];
  const offline = disks.filter(d => !d.online);
  if (offline.length) items.push({ key: 'offline', tone: 'crit', icon: WifiOff, title: `${plural(offline.length, 'storage location')} offline`, detail: offline.map(d => d.name).join(', '), action: 'Check storage', to: '/admin/storage' });
  const full = disks.filter(d => d.online && isLowSpace(d));
  if (full.length) items.push({ key: 'space', tone: 'warn', icon: HardDrive, title: `Low space on ${plural(full.length, 'disk')}`, detail: full.map(d => `${d.name}: ${formatBytes(d.availableCapacity)} free`).join(' · '), action: 'Open storage', to: '/admin/storage' });
  if (overview?.failedBackgroundJobs > 0) items.push({ key: 'failed', tone: 'crit', icon: AlertTriangle, title: `${plural(overview.failedBackgroundJobs, 'job')} failed this week`, detail: 'Open the job to see what went wrong and retry it.', action: 'Review jobs', to: '/admin/jobs?tab=history&status=FAILED' });
  const CHECKS = { providers: { icon: Plug, action: 'Configure', to: '/admin/metadata/providers' }, tools: { icon: Wrench, action: 'Details', to: '/admin/system' }, database: { icon: Database, action: 'Details', to: '/admin/system' } };
  for (const check of health?.checks || []) {
    const meta = CHECKS[check.id];
    if (!meta || check.status === 'ok') continue;
    items.push({ key: `check-${check.id}`, tone: check.status === 'error' ? 'crit' : 'warn', icon: meta.icon, title: check.label, detail: check.detail, action: meta.action, to: meta.to });
  }
  if (reviewCount > 0) items.push({ key: 'review', tone: 'warn', icon: ClipboardCheck, title: `${plural(reviewCount, 'title')} waiting for review`, detail: 'Providers suggested matches. Nothing is applied until you choose.', action: 'Review', to: '/admin/review' });
  if (overview?.duplicateGroups > 0) items.push({ key: 'dupes', tone: 'warn', icon: Copy, title: plural(overview.duplicateGroups, 'duplicate group'), detail: 'Identical files taking up space.', action: 'Review', to: '/admin/duplicates' });
  if (overview?.filesMissingMetadata > 0) items.push({ key: 'meta', tone: 'info', icon: Sparkles, title: `${plural(overview.filesMissingMetadata, 'title')} without metadata`, detail: 'Named after their files so far.', action: 'Match', to: '/admin/videos?status=filename' });
  return items.sort((a, b) => TONE_RANK[a.tone] - TONE_RANK[b.tone]);
}

function dayLabel(date) {
  return new Date(`${date}T00:00`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

function StatusBanner({ summary, health, loading, error, attentionCount }) {
  if (loading) return <div className="console-banner" role="status"><Status tone="neutral">Checking system status…</Status></div>;
  if (error && !health) return <div className="console-banner" data-tone="warn" role="status"><Status tone="warn">Status unavailable</Status><span className="console-banner__meta">The health check did not answer. {error.message}</span><Link className="console-banner__link console-section__link" to="/admin/system">System status</Link></div>;
  const tone = summary.tone;
  const title = tone === 'ok' ? (attentionCount ? 'All systems normal' : 'All systems normal') : tone === 'crit' ? `Something needs attention: ${summary.label}` : `Working, with ${summary.label}`;
  return <div className="console-banner" data-tone={tone === 'ok' ? undefined : tone} role="status">
    <Status tone={tone} pulse={tone === 'ok'}><span className="console-banner__title">{title}</span></Status>
    {health && <span className="console-banner__meta">Up {formatUptime(health.runtime.uptimeSeconds)} · {plural(health.streams.streams, 'stream')} playing · {plural(health.jobs.pending, 'job')} queued · database {health.database.latencyMs.toFixed(0)} ms</span>}
    <Link className="console-banner__link console-section__link" to="/admin/system">System status</Link>
  </div>;
}

function formatUptime(seconds) {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

function Attention({ items, loading }) {
  const navigate = useNavigate();
  if (loading) return <Panel title="Needs attention" bodyless><SkeletonRows rows={2} label="Loading alerts" /></Panel>;
  if (!items.length) return <Panel title="Needs attention" bodyless><EmptyState compact icon={CheckCircle2} title="Nothing needs attention" message="Storage is online, no job failed this week, and there is nothing waiting for review." /></Panel>;
  return <Panel title="Needs attention" subtitle={plural(items.length, 'item')} bodyless>
    <ul className="console-attention">
      {items.map(item => <li key={item.key} data-tone={item.tone}>
        <span className="console-attention__sev" aria-hidden="true" />
        <item.icon size={16} className="console-attention__icon" aria-hidden="true" />
        <div className="console-attention__copy">
          <p className="console-attention__title">{item.title}<span className="console-sr-only"> ({item.tone === 'crit' ? 'critical' : item.tone === 'warn' ? 'warning' : 'info'})</span></p>
          <p className="console-attention__detail">{item.detail}</p>
        </div>
        <Button size="sm" variant="ghost" onClick={() => navigate(item.to)} trailingIcon={<ArrowRight size={14} />}>{item.action}</Button>
      </li>)}
    </ul>
  </Panel>;
}

function ActivityChart() {
  const [range, setRange] = useState('30');
  const [series, setSeries] = useState('watch');
  const usage = useResource(() => getUsageAnalytics(Number(range), TIME_ZONE), [range]);
  const data = usage.data;
  const watch = data?.watch;
  const labels = (watch?.daily || []).map(d => dayLabel(d.date));
  const hasWatch = (watch?.hours || 0) >= 0.01;
  const added = data?.library?.daily || [];
  return <Panel title="Activity" subtitle={series === 'watch' ? 'Hours watched per day, across everyone.' : 'Titles added per day.'}
    actions={<>
      <RangeTabs label="Chart" value={series} onChange={setSeries} options={[{ value: 'watch', label: 'Watch time' }, { value: 'added', label: 'Added' }]} />
      <RangeTabs label="Time range" value={range} onChange={setRange} options={RANGES} />
    </>}>
    {usage.error && !data ? <ErrorState compact title="Couldn't load activity" error={usage.error} onRetry={usage.reload} />
      : !data ? <SkeletonRows rows={3} label="Loading activity" />
        : series === 'watch'
          ? (hasWatch
            ? <ConsoleBarChart series={[{ id: 'hours', label: 'Hours', data: watch.daily.map(d => Number(d.hours.toFixed(2))) }]} labels={labels} yFormat={v => `${v} h`} />
            : <EmptyState compact title="No watch time in this period" message={watch.sessions ? 'Plays started, but no watch time was recorded yet.' : 'Nobody has watched anything in this range.'} />)
          : (data.library.addedItems
            ? <ConsoleBarChart series={[{ id: 'items', label: 'Titles added', data: added.map(d => d.items) }]} labels={added.map(d => dayLabel(d.date))} />
            : <EmptyState compact title="Nothing added in this period" message="New titles show up here after a scan finds them." />)}
  </Panel>;
}

function Operations({ jobsApi, libraries, disks }) {
  const notify = useNotify();
  const [busy, setBusy] = useState(null);
  const jobs = jobsApi.data?.items || [];
  const running = jobs.filter(isActive);
  const scanning = new Set(running.filter(j => j.kind === 'SCAN' && j.libraryId).map(j => j.libraryId));

  async function scan(library) {
    setBusy(library.id);
    try { await startLibraryScan(library.id); notify.success(`Scanning ${library.name}`); await jobsApi.reload(); }
    catch (err) { notify.error('start the scan', err); }
    finally { setBusy(null); }
  }
  async function stop(job) {
    try { await cancelJob(job.id); notify.success(`${jobName(job)} stopped`); await jobsApi.reload(); }
    catch (err) { notify.error('stop the job', err); }
  }

  return <div className="console-grid-2">
    <Panel title="Running now" subtitle={running.length ? `${plural(running.length, 'job')} in progress` : undefined} actions={<PanelLink to="/admin/jobs">All jobs</PanelLink>} bodyless>
      {jobsApi.loading ? <SkeletonRows rows={2} /> : !running.length ? <EmptyState compact title="Nothing is running" message="Scans and other tasks show here while they work." />
        : <ul className="console-rows">
          {running.slice(0, 5).map(job => {
            const p = jobProgress(job);
            const Icon = jobIcon(job.kind);
            return <li key={job.id} className="console-row">
              <div className="console-row__main">
                <span className="console-row__title"><Icon size={14} aria-hidden="true" />{jobName(job)}<JobStatus status={job.status} /></span>
                <span className="console-row__meta">{p.label}{p.percent != null ? ` · ${p.percent}%` : ''}</span>
              </div>
              <div className="console-row__aside"><Button size="sm" variant="outline" onClick={() => stop(job)}>Stop</Button></div>
              <div className="console-row__bar"><ProgressBar percent={p.percent} label={`${jobName(job)} progress`} /></div>
            </li>;
          })}
        </ul>}
    </Panel>

    <Panel title="Libraries & storage" actions={<PanelLink to="/admin/libraries">Manage</PanelLink>} bodyless>
      {!libraries.length && !disks.length ? <EmptyState compact title="Nothing set up yet" action={<Button size="sm" asChild><Link to="/admin/setup">Open the setup guide</Link></Button>} />
        : <ul className="console-rows">
          {libraries.map(library => {
            const isScanning = library.scanning || scanning.has(library.id);
            const offline = library.roots.length > 0 && library.roots.every(root => disks.find(d => d.id === root.storageLocationId)?.online === false);
            return <li key={`l${library.id}`} className="console-row">
              <div className="console-row__main">
                <span className="console-row__title">{library.name}{offline && <Status tone="crit">Offline</Status>}</span>
                <span className="console-row__meta">{plural(library.mediaCount, 'title')} · {formatBytes(library.totalBytes)} · {isScanning ? 'scanning now' : library.lastScanAt ? `scanned ${timeAgo(library.lastScanAt)}` : 'never scanned'}</span>
              </div>
              <div className="console-row__aside">
                <Button size="sm" variant="ghost" aria-label={`Scan ${library.name}`} title={offline ? 'Storage is offline' : 'Scan now'} disabled={isScanning || busy === library.id || offline || !library.roots.length} onClick={() => scan(library)} leadingIcon={<RefreshCw size={14} className={isScanning ? 'console-spin' : ''} />}>Scan</Button>
              </div>
            </li>;
          })}
          {disks.map(disk => {
            const used = usedFraction(disk);
            return <li key={`d${disk.id}`} className="console-row">
              <div className="console-row__main">
                <span className="console-row__title"><HardDrive size={14} aria-hidden="true" />{disk.name}{!disk.online && <Status tone="crit">Offline</Status>}</span>
                <span className="console-row__meta">{used != null ? `${formatBytes(disk.availableCapacity)} free of ${formatBytes(disk.totalCapacity)}` : disk.mountPath}</span>
              </div>
              {used != null && <div className="console-row__aside"><span className="console-num console-small">{Math.round(used * 100)}% used</span></div>}
              {used != null && <div className="console-row__bar"><Meter fraction={used} label={`${disk.name} used space`} /></div>}
            </li>;
          })}
        </ul>}
    </Panel>
  </div>;
}

function RecentActivity({ jobsApi }) {
  const finished = (jobsApi.data?.items || []).filter(j => !isActive(j)).slice(0, 8);
  return <Panel title="Recent activity" actions={<PanelLink to="/admin/jobs?tab=history">History</PanelLink>} bodyless>
    {jobsApi.loading ? <SkeletonRows rows={3} /> : !finished.length ? <EmptyState compact title="Nothing has run yet" message="Scan a library to get started." />
      : <ul className="console-feed">
        {finished.map(job => <li key={job.id} className="console-feed__item" data-clickable>
          <span className="console-feed__dot" data-tone={job.status === 'FAILED' ? 'crit' : job.status === 'COMPLETED' ? 'ok' : undefined} aria-hidden="true" />
          <Link to={`/admin/jobs?tab=history&job=${job.id}`} className="console-feed__copy console-feed__link">
            <span className="console-feed__title">{jobName(job)} <JobStatus status={job.status} /></span>
            <span className="console-feed__detail">{resultSummary(job)}</span>
          </Link>
          <time className="console-feed__time">{timeAgo(job.finishedAt || job.createdAt)}</time>
        </li>)}
      </ul>}
  </Panel>;
}

export function DashboardPage() {
  const { health, healthError, healthLoading, summary, reviewCount } = useSignals();
  const jobsApi = useJobs({ limit: 30 });
  const overview = useResource(() => getOverview(), [], { interval: 30_000 });
  const analytics = useResource(() => getAnalytics(), []);
  const usage = useResource(() => getUsageAnalytics(14, TIME_ZONE), []);
  const libs = useResource(() => getLibraries().then(r => r.items || []), [], { interval: 30_000 });
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), [], { interval: 30_000 });
  const notify = useNotify();
  void notify;

  const diskList = disks.data || [];
  const attention = useMemo(() => attentionItems({ overview: overview.data, disks: diskList, reviewCount, health }), [overview.data, diskList, reviewCount, health]);
  const sized = diskList.filter(d => d.totalCapacity);
  const capacity = sized.reduce((t, d) => t + d.totalCapacity, 0);
  const used = sized.reduce((t, d) => t + (d.usedCapacity || 0), 0);
  const usedPercent = capacity ? Math.round((used / capacity) * 100) : null;
  const added14 = (analytics.data?.itemsAddedByDay || []).reduce((t, d) => t + d.count, 0);
  const metricsLoading = analytics.loading || usage.loading || disks.loading;
  const firstRun = disks.data && disks.data.length === 0;

  return <PageStack>
    <PageHeader title="Dashboard" subtitle="What is happening in ReelStash right now, and what needs you."
      actions={firstRun ? <Button asChild><Link to="/admin/setup"><Rocket size={14} /> Start the setup guide</Link></Button> : <Button variant="outline" asChild><Link to="/admin/analytics">Open analytics</Link></Button>} />

    <StatusBanner summary={summary} health={health} loading={healthLoading && !health} error={healthError} attentionCount={attention.length} />

    {overview.error && !overview.data ? <Panel bodyless><ErrorState title="Couldn't load the overview" error={overview.error} onRetry={overview.reload} /></Panel>
      : <Attention items={attention} loading={overview.loading || disks.loading} />}

    <MetricStrip label="Key metrics" loading={metricsLoading} items={[
      { label: 'Titles', value: formatNumber(analytics.data?.totalMediaItems), detail: added14 ? `+${formatNumber(added14)} in the last 14 days` : `${plural(analytics.data?.totalPerformers ?? 0, 'person', 'people')} · ${plural(analytics.data?.totalStudios ?? 0, 'studio')}`, to: '/admin/videos' },
      { label: 'Storage free', value: capacity ? formatBytes(capacity - used) : '—', detail: capacity ? `${usedPercent}% of ${formatBytes(capacity)} used · media ${formatBytes(analytics.data?.totalSizeBytes)}` : 'No capacity reported yet', tone: usedPercent >= 90 ? 'danger' : usedPercent >= 75 ? 'warning' : undefined, to: '/admin/storage' },
      { label: 'Watch time, 14 days', value: formatHours(usage.data?.watch.hours), detail: `${plural(usage.data?.watch.sessions ?? 0, 'play')}`, to: '/admin/analytics' },
      { label: 'Active viewers', value: formatNumber(usage.data?.users.active), detail: `of ${plural(usage.data?.users.total ?? 0, 'user')} · ${formatNumber(usage.data?.users.activeToday)} today`, to: '/admin/users' },
    ]} />

    <ActivityChart />
    <Operations jobsApi={jobsApi} libraries={libs.data || []} disks={diskList} />
    <RecentActivity jobsApi={jobsApi} />
  </PageStack>;
}
