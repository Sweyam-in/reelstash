import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Button } from '@spatika/react';
import { CircleStop, FolderOpen, HardDrive, Pencil, RefreshCw, Trash2, WandSparkles } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { cancelScan, getScanJobs, getStorageInsights, startScan } from '../../api';
import { jobProgress } from '../../pages/jobKinds';
import {
  Callout, ConsoleAreaChart, ConsoleBarChart, CopyButton, DetailList, EmptyState, ErrorState, Meter, MetricStrip, MoreMenu, PageHeader, PageStack, Panel, ProfileHeader, ProgressBar,
  RangeTabs, RankedList, SegmentedTabs, SkeletonRows, Status, rowActivation,
} from '../components';
import { formatBytes, formatDateTime, formatDuration, formatNumber, isLowSpace, plural, timeAgo } from '../lib/format';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';
import { useCrumb } from '../shell/Breadcrumbs';
import { DiskDialog, TYPE_LABEL, useDiskActions } from './StoragePage';

const SCAN_PHASE = { queued: 'Starting scan', walking: 'Searching the disk', indexing: 'Indexing files', finishing: 'Checking for missing files', matching: 'Looking up metadata', done: 'Scan finished' };
const RANGES = [{ value: '7', label: '7d' }, { value: '30', label: '30d' }, { value: '90', label: '90d' }, { value: '365', label: '1y' }];
const fileName = path => (path ? String(path).split('/').pop() || path : '');
const shortDate = (value, withTime) => { const d = new Date(value); return withTime ? d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric' }) : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }); };

function scanPercent(scan) {
  if (!scan || scan.completedAt) return null;
  if (scan.phase === 'finishing') return 100;
  if (scan.walkComplete && scan.filesSeen > 0) return Math.min(100, Math.round((scan.filesProcessed / scan.filesSeen) * 100));
  return null;
}
const progressLabel = scan => {
  if (!scan.completedAt) return scan.walkComplete ? `${scan.filesProcessed || 0} of ${scan.filesSeen || 0}` : scan.filesSeen ? `${scan.filesSeen} found` : '—';
  return !scan.filesSeen && !scan.filesProcessed ? '—' : `${scan.filesProcessed} indexed`;
};

export function DiskDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const notify = useNotify();
  const [filters, setFilters] = useUrlState({ tab: 'overview', days: '30' });
  const days = Number(filters.days) || 30;
  const insights = useResource(() => getStorageInsights(id, days), [id, days]);
  const scansApi = useResource(() => getScanJobs().then(r => (r.items || []).filter(s => String(s.storageLocationId) === id)), [id]);
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(false);
  const removeDisk = useDiskActions(async () => {});
  const data = insights.data;
  const disk = data?.location;
  useCrumb(disk ? disk.name : insights.error?.status === 404 ? 'Not found' : null);

  const scans = scansApi.data || [];
  const running = scans.find(s => !s.completedAt);
  // Scans poll quickly while one runs; the heavier insights reload once it finishes.
  const { reload: reloadScans } = scansApi;
  const { reload: reloadInsights } = insights;
  useEffect(() => {
    const t = setInterval(() => { if (document.visibilityState !== 'hidden') reloadScans(); }, running ? 2000 : 15000);
    return () => clearInterval(t);
  }, [running, reloadScans]);
  const wasRunning = useRef(false);
  useEffect(() => { if (wasRunning.current && !running) reloadInsights(); wasRunning.current = !!running; }, [running, reloadInsights]);

  async function toggleScan() {
    setBusy(true);
    try { await (running ? cancelScan(id) : startScan(id)); notify.success(running ? 'Scan cancelled' : 'Scan started'); setTimeout(reloadScans, 800); }
    catch (err) { notify.error(running ? 'cancel the scan' : 'start the scan', err); }
    finally { setBusy(false); }
  }

  if (insights.error && !data) {
    return <PageStack><PageHeader title="Storage location" back={{ to: '/admin/storage', label: 'All storage' }} /><Panel bodyless>
      {insights.error.status === 404 ? <EmptyState icon={HardDrive} title="There is no storage location with this ID" message="It may have been deleted, or the link is wrong." /> : <ErrorState title="Couldn't load this location" error={insights.error} onRetry={insights.reload} />}
    </Panel></PageStack>;
  }
  if (!data) return <PageStack><Panel bodyless><SkeletonRows rows={5} label="Loading storage location" /></Panel></PageStack>;

  const live = data.live;
  const total = live?.total ?? disk.totalCapacity;
  const available = live?.available ?? disk.availableCapacity;
  const usedNow = live?.used ?? disk.usedCapacity;
  const low = total && available / total < 0.1;
  const history = data.history || [];
  const added = data.added || [];
  const progress = scanPercent(running);
  const hourly = days <= 7;

  return <PageStack>
    <ProfileHeader mark={<HardDrive size={22} aria-hidden="true" />} title={disk.name}
      status={<Status tone={disk.online ? 'ok' : 'crit'}>{disk.online ? 'Online' : 'Offline'}</Status>}
      meta={<><span>{TYPE_LABEL[disk.type] || disk.type}</span><code className="console-code">{disk.mountPath}</code><CopyButton value={String(disk.id)} label="Location ID" display={`ID ${disk.id}`} /></>}
      actions={<>
        {running ? <Button variant="outline" disabled={busy} onClick={toggleScan} leadingIcon={<CircleStop size={14} />}>Stop scan</Button> : <Button disabled={busy} onClick={toggleScan} leadingIcon={<RefreshCw size={14} />}>Scan now</Button>}
        <MoreMenu items={[
          { label: 'Edit…', icon: Pencil, onSelect: () => setEditing(true) },
          { label: 'Organize files…', icon: WandSparkles, onSelect: () => navigate('/admin/organizer') },
          { label: 'Browse files', icon: FolderOpen, onSelect: () => window.location.assign(`/files/${id}`) },
          'separator',
          { label: 'Delete…', icon: Trash2, danger: true, onSelect: async () => { if (await removeDisk(disk)) navigate('/admin/storage'); } },
        ]} />
      </>} />

    {data.liveError && <Callout tone="warn" icon={<HardDrive />} title="Couldn’t read the disk just now">{data.liveError}. Showing the last known capacity.</Callout>}
    {!disk.online && <Callout tone="crit" icon={<HardDrive />} title="This location is offline">ReelStash can’t read it. Titles stored here are unavailable until it comes back; library scans skip it.</Callout>}

    <SegmentedTabs page label="Storage location sections" value={filters.tab} onChange={tab => setFilters({ tab })} options={[{ value: 'overview', label: 'Overview' }, { value: 'scans', label: 'Scans', count: scans.length }, { value: 'details', label: 'Details' }]} />

    {filters.tab === 'overview' && <>
      <MetricStrip label="Storage figures" items={[
        { label: 'Free space', value: formatBytes(available), detail: total ? `${Math.round((available / total) * 100)}% of ${formatBytes(total)}${low ? ' · low' : ''}` : undefined, tone: low ? 'warning' : undefined },
        { label: 'Library on disk', value: formatBytes(data.mediaBytes), detail: `${plural(data.titles, 'title')} · ${formatDuration(data.durationSeconds)}` },
        { label: 'Video files', value: formatNumber(data.availableFiles), detail: data.missingFiles ? `${formatNumber(data.missingFiles)} missing` : 'None missing', tone: data.missingFiles ? 'warning' : undefined },
        { label: 'Scan rules', value: formatNumber(data.rules), detail: data.rules ? 'Paths skipped by scans' : 'Nothing excluded', to: '/admin/scan-rules' },
      ]} />
      <div className="console-grid-2">
        <Panel title="Space" subtitle="How the disk is used right now.">
          {!total ? <p className="console-muted console-small">Capacity not read yet.</p> : <>
            <Meter fraction={Math.min(1, usedNow / total)} label="Used space" />
            <DetailList flush items={(() => { const library = Math.min(data.mediaBytes, usedNow); return [
              { term: 'Library videos', details: <span className="console-num">{formatBytes(library)} · {Math.round((library / total) * 100)}%</span> },
              { term: 'Other files', details: <span className="console-num">{formatBytes(Math.max(0, usedNow - library))} · {Math.round((Math.max(0, usedNow - library) / total) * 100)}%</span> },
              { term: 'Free', details: <span className="console-num">{formatBytes(available)} · {Math.round((available / total) * 100)}%</span> },
            ]; })()} />
          </>}
        </Panel>
        <Panel title="Properties" bodyless>
          <DetailList flush items={[
            { term: 'Type', details: TYPE_LABEL[disk.type] || disk.type },
            { term: 'Mount path', details: <code className="console-code">{disk.mountPath}</code> },
            { term: 'Filesystem', details: data.mount?.fsType || '—' },
            { term: 'Device', details: data.mount?.device ? <code className="console-code">{data.mount.device}</code> : '—' },
            { term: 'Access', details: data.mount ? (data.mount.readOnly ? 'Read-only' : 'Read and write') : '—' },
            { term: 'Last seen', details: data.lastSeenAt ? timeAgo(data.lastSeenAt) : '—' },
            { term: 'Last scan', details: disk.lastScanAt ? timeAgo(disk.lastScanAt) : 'Never' },
          ]} />
        </Panel>
      </div>
      <Panel title="Used space over time" subtitle="Read every hour. History starts when ReelStash first measured this disk." actions={<RangeTabs label="Time range" value={String(days)} onChange={v => setFilters({ days: v })} options={RANGES} />}>
        {history.length < 2 ? <EmptyState compact title={history.length ? 'One reading so far' : 'No readings yet'} message={history.length ? 'The chart fills in as hourly readings arrive.' : 'The first reading is taken within the hour.'} />
          : <ConsoleAreaChart series={[{ id: 'used', label: 'Used', data: history.map(h => h.used), area: true }]} labels={history.map(h => shortDate(h.at, hourly))} yFormat={formatBytes} yMax={Math.max(...history.map(h => h.total))} />}
      </Panel>
      <Panel title="Videos added" subtitle="Size of new video files found by scans, per day.">
        {!added.some(d => d.bytes) ? <EmptyState compact title="Nothing added in this period" /> : <ConsoleBarChart height={200} series={[{ id: 'bytes', label: 'Added', data: added.map(d => d.bytes) }]} labels={added.map(d => shortDate(`${d.date}T00:00`))} yFormat={formatBytes} />}
      </Panel>
      <div className="console-rankgrid">
        <Panel title="Formats"><RankedList items={data.byFormat} value={i => i.bytes} format={(b, i) => `${formatBytes(b)} · ${i.count}`} /></Panel>
        <Panel title="Quality"><RankedList items={data.byQuality} value={i => i.bytes} format={(b, i) => `${formatBytes(b)} · ${i.count}`} /></Panel>
        <Panel title="Largest folders" subtitle="Top-level folders by video size."><RankedList items={data.topFolders} value={i => i.bytes} format={(b, i) => `${formatBytes(b)} · ${i.count}`} /></Panel>
      </div>
    </>}

    {filters.tab === 'scans' && <>
      {running && <Panel title="Live scan" subtitle={SCAN_PHASE[running.phase] || 'Scan running'} actions={<Status tone="accent" pulse>{progress == null ? 'Searching' : `${progress}%`}</Status>}>
        <ProgressBar percent={progress} label="Scan progress" />
        <DetailList flush items={[
          { term: 'Indexed', details: running.filesProcessed || 0 }, { term: 'Found', details: running.filesSeen || 0 }, { term: 'New', details: running.discoveredCount || 0 }, { term: 'Changed', details: running.changedCount || 0 },
          running.currentPath && { term: 'Current file', details: <span title={running.currentPath}>{fileName(running.currentPath)}</span> },
        ]} />
        {progress == null && !running.filesSeen && <p className="console-muted console-small">Waiting for the first file. A quiet or offline disk can sit here until the mount responds.</p>}
      </Panel>}
      <Panel title="Recent scans" bodyless>
        {scansApi.loading ? <SkeletonRows rows={3} /> : scans.length === 0 ? <EmptyState compact title="No scans yet" message="Scan this location to index its videos." />
          : <div className="console-table-responsive"><Table className="console-stack-table"><TableHeader><TableRow><TableHead>Started</TableHead><TableHead className="console-num-cell">New</TableHead><TableHead className="console-num-cell">Changed</TableHead><TableHead className="console-num-cell">Missing</TableHead><TableHead>Progress</TableHead><TableHead>Finished</TableHead></TableRow></TableHeader>
            <TableBody>{scans.map(s => <TableRow key={s.id}>
              <TableCell className="console-nowrap">{formatDateTime(s.startedAt)}</TableCell><TableCell className="console-num-cell">{s.discoveredCount}</TableCell><TableCell className="console-num-cell">{s.changedCount}</TableCell><TableCell className="console-num-cell">{s.missingCount}</TableCell>
              <TableCell className="console-num">{progressLabel(s)}</TableCell>
              <TableCell className="console-nowrap">{s.completedAt ? formatDateTime(s.completedAt) : <Status tone="accent" pulse>{SCAN_PHASE[s.phase] || 'Running'}</Status>}</TableCell>
            </TableRow>)}</TableBody></Table></div>}
      </Panel>
    </>}

    {filters.tab === 'details' && <Panel title="Configuration" bodyless footer={<><span>Changing the mount path does not move any files.</span><Button size="sm" variant="outline" onClick={() => setEditing(true)} leadingIcon={<Pencil size={14} />}>Edit name and path</Button></>}>
      <DetailList flush items={[{ term: 'Name', details: disk.name }, { term: 'Type', details: TYPE_LABEL[disk.type] || disk.type }, { term: 'Mount path', details: <code className="console-code">{disk.mountPath}</code> }, { term: 'Location ID', details: <CopyButton value={String(disk.id)} label="Location ID" /> }]} />
    </Panel>}
    <DiskDialog open={editing} onOpenChange={setEditing} disk={disk} onSaved={insights.reload} />
  </PageStack>;
}
