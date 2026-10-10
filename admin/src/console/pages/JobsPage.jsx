import React, { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from '@spatika/react';
import { ChevronDown, CircleStop, ListChecks, Play, RotateCcw, Trash2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { cancelJob, clearFinishedJobs, getBackgroundJob, getBackgroundJobs, getLibraries, retryJob, runJob } from '../../api';
import { JOB_KINDS, RUNNABLE_KINDS, isActive, jobIcon, jobName, jobProgress, jobTitle, resultSummary } from '../../pages/jobKinds';
import {
  BulkBar, DetailDrawer, DetailList, DrawerSection, EmptyState, ErrorState, JobStatus, PageHeader, PageStack, Panel, ProgressBar, SegmentedTabs, SkeletonRows,
  Status, TablePagination, TableToolbar, rowActivation, useConfirm,
} from '../components';
import { formatDateTime, formatDuration, plural, timeAgo } from '../lib/format';
import { useJobs } from '../lib/useJobs';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';
import { useCrumb } from '../shell/Breadcrumbs';

const FILTERS = [['all', 'All'], ['active', 'Active'], ['FAILED', 'Failed'], ['COMPLETED', 'Completed'], ['CANCELLED', 'Stopped']];
const PAGE_DEFAULT = 25;
const STATUS_FILTERS = ['FAILED', 'COMPLETED', 'CANCELLED'];

// Start button for one task. Per-library tasks ask which library first.
function RunButton({ kind, libraries, busy, onRun }) {
  const def = JOB_KINDS[kind];
  const label = <><Play size={14} /> Run</>;
  if (!def.perLibrary) return <Button size="sm" variant="outline" disabled={busy} onClick={() => onRun(kind, 0)}>{label}</Button>;
  if (libraries.length === 0) return <Button size="sm" variant="outline" disabled title="Create a library first">{label}</Button>;
  if (libraries.length === 1) return <Button size="sm" variant="outline" disabled={busy} onClick={() => onRun(kind, libraries[0].id)}>{label}</Button>;
  return <DropdownMenu>
    <DropdownMenuTrigger asChild><Button size="sm" variant="outline" disabled={busy} trailingIcon={<ChevronDown size={14} />}>{label}</Button></DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuLabel>Run on…</DropdownMenuLabel>
      {libraries.map(library => <DropdownMenuItem key={library.id} onSelect={() => onRun(kind, library.id)}>{library.name}</DropdownMenuItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}

function JobDrawer({ jobId, jobs, onClose, onStop, onRetry }) {
  const known = jobs.find(j => j.id === jobId);
  // A job linked from elsewhere may have scrolled out of the recent list: ask for it by id.
  const fetched = useResource(() => getBackgroundJob(jobId), [jobId], { enabled: !!jobId && !known });
  const job = known || fetched.data;
  const scan = job?.scan;
  return <DetailDrawer open={!!jobId} onClose={onClose} eyebrow={job ? `Job #${job.id}` : 'Job'} title={job ? jobName(job) : 'Job'}
    description={job ? <JobStatus status={job.status} /> : undefined}
    footer={job && (isActive(job)
      ? <Button variant="outline" onClick={() => onStop(job)} leadingIcon={<CircleStop size={14} />}>Stop</Button>
      : job.kind in JOB_KINDS && <Button variant="outline" onClick={() => onRetry(job)} leadingIcon={<RotateCcw size={14} />}>{job.status === 'COMPLETED' ? 'Run again' : 'Retry'}</Button>)}>
    {!job ? (fetched.error ? <ErrorState compact title="Couldn't load the job" error={fetched.error} onRetry={fetched.reload} /> : <SkeletonRows rows={3} />) : <>
      {job.error && <DrawerSection title="Error"><pre className="console-pre">{job.error}</pre></DrawerSection>}
      {isActive(job) && <DrawerSection title="Progress">
        <ProgressBar percent={jobProgress(job).percent} label="Job progress" />
        <p className="console-muted console-small">{jobProgress(job).label}{jobProgress(job).detail ? ` · ${jobProgress(job).detail}` : ''}</p>
      </DrawerSection>}
      <DrawerSection title="Outcome"><p className="console-small">{resultSummary(job) || 'Still running.'}</p></DrawerSection>
      {scan && <DrawerSection title="Scan"><DetailList items={[
        { term: 'Files found', details: scan.filesSeen }, { term: 'Indexed', details: scan.filesProcessed }, { term: 'New', details: scan.discovered },
        { term: 'Changed', details: scan.changed }, { term: 'Missing', details: scan.missing },
      ]} /></DrawerSection>}
      <DrawerSection title="Details"><DetailList items={[
        { term: 'Created', details: formatDateTime(job.createdAt) },
        job.startedAt && { term: 'Started', details: formatDateTime(job.startedAt) },
        job.finishedAt && { term: 'Finished', details: formatDateTime(job.finishedAt) },
        job.startedAt && { term: 'Took', details: formatDuration(job.durationSeconds) },
        { term: 'Attempts', details: job.attempts },
        job.libraryName && { term: 'Library', details: job.libraryName },
        job.storageLocationName && { term: 'Disk', details: job.storageLocationName },
      ]} /></DrawerSection>
      {job.result && <DrawerSection title="Raw result"><pre className="console-pre">{JSON.stringify(job.result, null, 2)}</pre></DrawerSection>}
    </>}
  </DetailDrawer>;
}

export function JobsPage() {
  const [filters, setFilters] = useUrlState({ tab: 'tasks', status: 'all', kind: '', job: '', page: '0', size: String(PAGE_DEFAULT) });
  const [, setParams] = useSearchParams();
  const jobsApi = useJobs({ limit: 100 });
  const libs = useResource(() => getLibraries().then(r => r.items || []), []);
  const notify = useNotify();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(null);
  const [selected, setSelected] = useState([]);
  useCrumb(null);

  const data = jobsApi.data || { items: [], counts: {}, latestByKind: {} };
  const { items, counts, latestByKind } = data;
  const active = items.filter(isActive);
  const libraries = libs.data || [];
  const finishedCount = (counts.COMPLETED || 0) + (counts.FAILED || 0) + (counts.CANCELLED || 0);
  const page = Number(filters.page) || 0;
  const size = Number(filters.size) || PAGE_DEFAULT;

  // History pages on the server (by status and task). "Active" is the live list, which is short.
  const serverStatus = STATUS_FILTERS.includes(filters.status) ? filters.status : '';
  const history = useResource(() => getBackgroundJobs({ status: serverStatus, kind: filters.kind, limit: size, offset: page * size }), [serverStatus, filters.kind, page, size],
    { enabled: filters.tab === 'history' && filters.status !== 'active', interval: 10_000 });
  const liveActive = items.filter(j => isActive(j) && (!filters.kind || j.kind === filters.kind));
  const paged = filters.status === 'active' ? liveActive.slice(page * size, page * size + size) : (history.data?.items || []);
  const shownTotal = filters.status === 'active' ? liveActive.length : (history.data?.total ?? 0);
  const historyLoading = filters.status !== 'active' && history.loading;
  // The history refreshes when the live list says something finished.
  const finishedKey = `${counts.COMPLETED || 0}/${counts.FAILED || 0}/${counts.CANCELLED || 0}`;
  const { reload: reloadHistory } = history;
  useEffect(() => { if (filters.tab === 'history') reloadHistory(); }, [finishedKey]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setSelected([]); }, [filters.status, filters.kind, page, size]);

  async function handleRun(kind, libraryId) {
    const def = JOB_KINDS[kind];
    let reason;
    if (def.confirm) {
      const ok = await confirm({ title: `${def.title}?`, consequence: def.confirm, affects: libraryId ? libraries.find(l => l.id === libraryId)?.name : 'Every library', reversible: false, confirmLabel: def.title, tone: 'danger', typeToConfirm: 'remove', reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.' } });
      if (!ok) return;
      reason = ok.reason;
    }
    setBusy(kind);
    try { await runJob(kind, libraryId, { reason }); notify.success(`${def.title} started`); await jobsApi.reload(); }
    catch (err) { notify.error(`start ${def.title.toLowerCase()}`, err); }
    finally { setBusy(null); }
  }
  async function handleStop(job) {
    try { await cancelJob(job.id); notify.success(`${jobName(job)} stopped`); await jobsApi.reload(); }
    catch (err) { notify.error('stop the job', err); }
  }
  async function handleRetry(job) {
    try { await retryJob(job.id); notify.success(`${jobTitle(job.kind)} started`); await jobsApi.reload(); }
    catch (err) { notify.error('start the job', err); }
  }
  async function stopSelected() {
    const targets = items.filter(j => selected.includes(j.id) && isActive(j));
    const results = await Promise.allSettled(targets.map(j => cancelJob(j.id)));
    const failed = results.filter(r => r.status === 'rejected').length;
    if (failed) notify.error(`stop ${plural(failed, 'job')}`, results.find(r => r.status === 'rejected').reason); else notify.success(`Stopped ${plural(targets.length, 'job')}`);
    setSelected([]);
    await jobsApi.reload();
  }
  async function handleClear() {
    const ok = await confirm({ title: 'Clear finished jobs?', consequence: `Removes ${plural(finishedCount, 'finished job')} from the history. Running and queued jobs are kept. Nothing on disk changes.`, affects: 'The job history list only', reversible: false, confirmLabel: 'Clear history' });
    if (!ok) return;
    try { const r = await clearFinishedJobs(); notify.success(`Cleared ${plural(r.deleted, 'finished job')}`); await jobsApi.reload(); }
    catch (err) { notify.error('clear the history', err); }
  }

  const setTab = tab => setFilters({ tab, job: '' });
  const openJob = id => setFilters({ job: String(id) }, { keepPage: true });
  const closeJob = () => setParams(prev => { const next = new URLSearchParams(prev); next.delete('job'); return next; }, { replace: true });

  return <PageStack>
    <PageHeader title="Jobs" subtitle="Start, watch and stop everything ReelStash does in the background."
      actions={<Status tone={jobsApi.connected ? 'ok' : 'warn'} pulse={jobsApi.connected}>{jobsApi.connected ? 'Live' : 'Polling every few seconds'}</Status>} />

    {jobsApi.error && !jobsApi.data ? <Panel bodyless><ErrorState title="Couldn't load jobs" error={jobsApi.error} onRetry={jobsApi.reload} /></Panel> : <>
      <SegmentedTabs page label="Jobs and history" value={filters.tab} onChange={setTab} options={[{ value: 'tasks', label: 'Tasks' }, { value: 'history', label: 'History', count: finishedCount + active.length }]} />

      {filters.tab === 'tasks' ? <>
        {active.length > 0 && <Panel title="Running now" subtitle={plural(active.length, 'job')} bodyless>
          <ul className="console-rows">
            {active.map(job => {
              const p = jobProgress(job);
              const Icon = jobIcon(job.kind);
              return <li key={job.id} className="console-row" data-clickable onClick={() => openJob(job.id)}>
                <div className="console-row__main">
                  <span className="console-row__title"><Icon size={14} aria-hidden="true" />{jobName(job)}<JobStatus status={job.status} /></span>
                  <span className="console-row__meta console-truncate" title={p.detail}>{p.label}{p.percent != null ? ` · ${p.percent}%` : ''}{p.detail ? ` · ${p.detail}` : ''} · started {timeAgo(job.startedAt || job.createdAt)}</span>
                </div>
                <div className="console-row__aside"><Button size="sm" variant="outline" leadingIcon={<CircleStop size={14} />} onClick={e => { e.stopPropagation(); handleStop(job); }}>Stop</Button></div>
                <div className="console-row__bar"><ProgressBar percent={p.percent} label={`${jobName(job)} progress`} /></div>
              </li>;
            })}
          </ul>
        </Panel>}

        <Panel title="Tasks" subtitle="Run any of these on demand." bodyless>
          {jobsApi.loading ? <SkeletonRows rows={6} /> : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
            <TableHeader><TableRow><TableHead>Task</TableHead><TableHead>Last run</TableHead><TableHead className="console-wrap">What it does</TableHead><TableHead><span className="console-sr-only">Run</span></TableHead></TableRow></TableHeader>
            <TableBody>
              {RUNNABLE_KINDS.map(kind => {
                const def = JOB_KINDS[kind];
                const latest = latestByKind?.[kind];
                const running = active.filter(j => j.kind === kind);
                return <TableRow key={kind}>
                  <TableCell><span className="console-title-cell"><def.icon size={14} aria-hidden="true" /><strong>{def.title}</strong></span></TableCell>
                  <TableCell data-label="Last run:">{running.length ? <Status tone="accent" pulse>{running.length > 1 ? `${running.length} running` : running[0].status === 'PENDING' ? 'Queued' : 'Running now'}</Status>
                    : latest ? <span className="console-nowrap"><JobStatus status={latest.status} /> <span className="console-muted console-small">{timeAgo(latest.finishedAt || latest.createdAt)}</span></span> : <span className="console-muted">Never run</span>}</TableCell>
                  <TableCell className="console-wrap console-muted console-small console-desc">{def.description}</TableCell>
                  <TableCell>{running.length && (!def.perLibrary || libraries.length <= 1)
                    ? <Button size="sm" variant="outline" leadingIcon={<CircleStop size={14} />} onClick={() => running.forEach(handleStop)}>Stop</Button>
                    : <RunButton kind={kind} libraries={libraries} busy={busy === kind} onRun={handleRun} />}</TableCell>
                </TableRow>;
              })}
            </TableBody>
          </Table></div>}
        </Panel>
      </> : <Panel bodyless footer={(data.thumbnailsPending > 0 || data.previewsPending > 0) ? [data.thumbnailsPending > 0 && `${plural(data.thumbnailsPending, 'thumbnail')} generating in the background`, data.previewsPending > 0 && `${plural(data.previewsPending, 'preview')} generating in the background`].filter(Boolean).join(' · ') : undefined}>
        <SegmentedTabs label="Filter by status" value={filters.status} onChange={status => setFilters({ status })}
          options={FILTERS.map(([value, label]) => ({ value, label, count: value === 'all' ? items.length : value === 'active' ? active.length : counts[value] || 0 }))} />
        <TableToolbar end={finishedCount > 0 && <Button size="sm" variant="ghost" leadingIcon={<Trash2 size={14} />} onClick={handleClear}>Clear finished</Button>}>
          <select className="console-select" aria-label="Filter by task" value={filters.kind} onChange={e => setFilters({ kind: e.target.value })}>
            <option value="">All tasks</option>
            {[...new Set([...Object.keys(JOB_KINDS), ...items.map(j => j.kind)])].map(kind => <option key={kind} value={kind}>{jobTitle(kind)}</option>)}
          </select>
          
        </TableToolbar>
        <BulkBar count={selected.length} noun="jobs" onClear={() => setSelected([])}>
          <Button size="sm" variant="outline" disabled={!items.some(j => selected.includes(j.id) && isActive(j))} leadingIcon={<CircleStop size={14} />} onClick={stopSelected}>Stop selected</Button>
        </BulkBar>
        {historyLoading || jobsApi.loading ? <SkeletonRows rows={5} /> : history.error && !history.data && filters.status !== 'active' ? <ErrorState title="Couldn't load the history" error={history.error} onRetry={history.reload} /> : shownTotal === 0
          ? <EmptyState icon={ListChecks} title={finishedCount + active.length === 0 ? 'No jobs yet' : 'Nothing matches these filters'} message={finishedCount + active.length === 0 ? 'Run a task from the Tasks tab and it shows up here.' : 'Try another status or task.'} />
          : <div className="console-table-responsive"><Table className="console-stack-table">
            <TableHeader><TableRow>
              <TableHead className="console-table-check"><input type="checkbox" aria-label="Select all jobs on this page" checked={paged.length > 0 && paged.every(j => selected.includes(j.id))} onChange={e => setSelected(e.target.checked ? paged.map(j => j.id) : [])} /></TableHead>
              <TableHead>Task</TableHead><TableHead>Status</TableHead><TableHead>Outcome</TableHead><TableHead>Started</TableHead><TableHead>Took</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {paged.map(job => {
                const Icon = jobIcon(job.kind);
                return <TableRow key={job.id} {...rowActivation(() => openJob(job.id), { selected: selected.includes(job.id) })} aria-label={`${jobName(job)}, ${job.status}`}>
                  <TableCell className="console-table-check"><input type="checkbox" aria-label={`Select ${jobName(job)}`} checked={selected.includes(job.id)} onChange={e => setSelected(s => e.target.checked ? [...s, job.id] : s.filter(x => x !== job.id))} /></TableCell>
                  <TableCell><span className="console-title-cell"><Icon size={14} aria-hidden="true" /><strong>{jobName(job)}</strong></span></TableCell>
                  <TableCell><JobStatus status={job.status} /></TableCell>
                  <TableCell className="console-truncate-cell console-muted" title={resultSummary(job)}>{isActive(job) ? jobProgress(job).label : resultSummary(job)}</TableCell>
                  <TableCell className="console-nowrap console-muted" title={formatDateTime(job.createdAt)}>{timeAgo(job.startedAt || job.createdAt)}</TableCell>
                  <TableCell className="console-num-cell">{job.startedAt ? formatDuration(job.durationSeconds) : '—'}</TableCell>
                </TableRow>;
              })}
            </TableBody>
          </Table></div>}
        {shownTotal > 0 && <TablePagination limit={size} offset={page * size} total={shownTotal} noun="jobs" onChange={({ limit, offset }) => setFilters({ size: String(limit), page: String(offset / limit) })} />}
      </Panel>}
    </>}
    <JobDrawer jobId={filters.job ? Number(filters.job) : null} jobs={[...items, ...paged]} onClose={closeJob} onStop={handleStop} onRetry={handleRetry} />
  </PageStack>;
}
