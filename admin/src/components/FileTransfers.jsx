import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowRightLeft, Check, TriangleAlert, X } from 'lucide-react';
import { CircularProgress, Popover, PopoverContent, PopoverTrigger, Progress, useToast } from '@spatika/react';
import { cancelJob, getBackgroundJobs, startFileTransfer } from '../api';
import { useEventsConnected, useServerEvent } from '../events';
import { TRANSFER_KIND, isActiveTransfer, overallPercent, transferProgress, transferTitle, transferToast } from '../fileTransfers';

const POLL_MS = 1000;

/**
 * Background pastes (FILE_TRANSFER jobs). Follows the admin jobs feed and
 * falls back to polling while a transfer runs and the feed is down. Only
 * transfers seen running in this session are listed; finished ones stay until
 * cleared. `finished` bumps (with the job) whenever one ends, so the browser
 * can refresh.
 */
export function useFileTransfers(enabled) {
  const { toast } = useToast();
  const connected = useEventsConnected();
  const [jobs, setJobs] = useState([]);
  const [finished, setFinished] = useState(null);
  const shown = useRef(new Set()); // ids seen active this session
  const dismissed = useRef(new Set());
  const last = useRef(new Map()); // id -> status

  const accept = useCallback(items => {
    const transfers = (items || []).filter(job => job.kind === TRANSFER_KIND);
    for (const job of transfers) {
      const before = last.current.get(job.id);
      if (isActiveTransfer(job)) shown.current.add(job.id);
      else if (shown.current.has(job.id) && (before === 'PENDING' || before === 'RUNNING')) {
        toast(transferToast(job));
        setFinished({ job, at: Date.now() });
      }
      last.current.set(job.id, job.status);
    }
    setJobs(transfers.filter(job => shown.current.has(job.id) && !dismissed.current.has(job.id)));
  }, [toast]);

  const reload = useCallback(() => {
    if (!enabled) return Promise.resolve();
    return getBackgroundJobs({ kind: TRANSFER_KIND, limit: 30 }).then(result => accept(result.items)).catch(() => {});
  }, [enabled, accept]);

  useEffect(() => { reload(); }, [reload]);
  useServerEvent(enabled ? 'jobs' : null, snapshot => accept(snapshot?.items));

  const active = jobs.some(isActiveTransfer);
  useEffect(() => {
    if (!enabled || !active || connected) return undefined;
    const timer = setInterval(reload, POLL_MS);
    return () => clearInterval(timer);
  }, [enabled, active, connected, reload]);

  const start = useCallback(async request => {
    const { backgroundJobId } = await startFileTransfer(request);
    shown.current.add(backgroundJobId);
    last.current.set(backgroundJobId, 'PENDING');
    await reload();
    return backgroundJobId;
  }, [reload]);

  const cancel = useCallback(async id => {
    try { await cancelJob(id); } catch (err) { toast({ title: 'Could not stop the transfer', description: err.message, tone: 'danger' }); }
    reload();
  }, [reload, toast]);

  const clearFinished = useCallback(() => {
    jobs.filter(job => !isActiveTransfer(job)).forEach(job => dismissed.current.add(job.id));
    setJobs(list => list.filter(isActiveTransfer));
  }, [jobs]);

  return { jobs, finished, start, cancel, clearFinished };
}

function TransferRow({ job, onCancel }) {
  const { percent, detail } = transferProgress(job);
  const active = isActiveTransfer(job);
  const tone = job.status === 'FAILED' ? 'danger' : job.status === 'CANCELLED' || job.result?.failures?.length ? 'warning' : active ? 'accent' : 'success';
  return <li className="rs-fm-xfer-row">
    <div className="rs-fm-xfer-head">
      <span className="rs-fm-xfer-title" title={transferTitle(job)}>{transferTitle(job)}</span>
      {active && <button type="button" className="rs-fm-navbtn" aria-label="Stop transfer" title="Stop" onClick={() => onCancel(job.id)}><X size={14} /></button>}
    </div>
    {(active || percent != null) && <Progress size="xs" tone={tone} value={percent} variant={percent == null ? 'indeterminate' : 'determinate'} />}
    <span className={`rs-fm-xfer-detail${job.status === 'FAILED' ? ' is-error' : ''}`} title={detail}>{detail}</span>
  </li>;
}

/** Header button: a progress ring while pastes run, a tick (or warning) once they finish. */
export function FileTransfersButton({ transfers }) {
  const { jobs, cancel, clearFinished } = transfers;
  if (!jobs.length) return null;
  const running = jobs.filter(isActiveTransfer);
  const percent = overallPercent(running);
  const problem = jobs.some(job => job.status === 'FAILED' || job.status === 'CANCELLED' || job.result?.failures?.length);
  const label = running.length ? `${running.length} transfer${running.length === 1 ? '' : 's'} running${percent != null ? ` · ${percent}%` : ''}` : 'Transfers finished';
  return <Popover>
    <PopoverTrigger asChild>
      <button type="button" className="rs-fm-xfer-btn" aria-label={label} title={label}>
        {running.length
          ? <span className="rs-fm-xfer-ring"><CircularProgress size={22} thickness={2.5} variant={percent == null ? 'indeterminate' : 'determinate'} value={percent ?? undefined} /><ArrowRightLeft size={10} /></span>
          : problem ? <TriangleAlert size={17} className="rs-fm-xfer-warn" /> : <Check size={17} className="rs-fm-xfer-ok" />}
      </button>
    </PopoverTrigger>
    <PopoverContent align="end" sideOffset={6} className="rs-fm-xfer-menu">
      <div className="rs-fm-xfer-top">
        <strong>Transfers</strong>
        {jobs.length > running.length && <button type="button" className="rs-fm-xfer-clear" onClick={clearFinished}>Clear finished</button>}
      </div>
      <ul>{jobs.map(job => <TransferRow key={job.id} job={job} onCancel={cancel} />)}</ul>
    </PopoverContent>
  </Popover>;
}
