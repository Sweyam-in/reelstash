import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@spatika/react';
import { CircleStop, Copy, Undo2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { cancelDuplicateScan, dismissDuplicate, getDuplicateScan, getDuplicates, getTrash, keepDuplicate, restoreTrash, startDuplicateScan } from '../../api';
import { mediaHref } from '../../appLinks';
import { EmptyState, ErrorState, PageHeader, PageStack, Panel, ProgressBar, SkeletonRows, Status, TableToolbar, useConfirm } from '../components';
import { formatBytes, plural } from '../lib/format';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

const fileName = path => (path ? String(path).split(/[/\\]/).pop() || path : '');

function scanText(job) {
  if (!job) return '';
  if (job.status === 'PENDING') return 'Starting duplicate scan…';
  if (job.status === 'RUNNING' && job.phase === 'grouping') return 'Grouping duplicates…';
  if (job.status === 'RUNNING') { const current = fileName(job.currentPath); const counts = `${job.hashed || 0} of ${job.total || 0} hashed`; return current ? `Hashing files (${counts}) · ${current}` : `Hashing files (${counts})`; }
  if (job.status === 'FAILED') return job.error || 'Duplicate scan failed';
  if (job.status === 'CANCELLED') return 'Duplicate scan stopped';
  if (job.status === 'COMPLETED') return `Last scan finished. Hashed ${job.hashed || 0} of ${job.total || 0}${job.failed ? `, ${job.failed} failed` : ''}.`;
  return job.status;
}

export function DuplicatesPage() {
  const { libraryId, libraries } = useAdminLibrary();
  const notify = useNotify();
  const confirm = useConfirm();
  const groups = useResource(() => getDuplicates(libraryId ? { libraryId } : {}).then(r => r.items || []), [libraryId]);
  const trash = useResource(() => getTrash().then(r => r.items || []).catch(() => []), []);
  const [scan, setScan] = useState(null);
  const [starting, setStarting] = useState(false);
  const running = !!scan && (scan.status === 'PENDING' || scan.status === 'RUNNING');
  const previous = useRef(null);

  // Poll the scan every 2 seconds only while one runs; otherwise just check now and then.
  useEffect(() => {
    let live = true;
    const tick = () => getDuplicateScan().then(r => { if (live) setScan(r.job || null); }).catch(() => {});
    tick();
    const t = setInterval(() => { if (document.visibilityState !== 'hidden') tick(); }, running ? 2000 : 20000);
    return () => { live = false; clearInterval(t); };
  }, [running]);
  const { reload: reloadGroups } = groups;
  useEffect(() => {
    const status = scan?.status || null;
    if ((previous.current === 'PENDING' || previous.current === 'RUNNING') && status === 'COMPLETED') reloadGroups();
    previous.current = status;
  }, [scan, reloadGroups]);

  async function start() {
    setStarting(true);
    try { await startDuplicateScan(); notify.success('Duplicate scan started'); setScan((await getDuplicateScan()).job || null); }
    catch (err) { notify.error('start the duplicate scan', err); }
    finally { setStarting(false); }
  }
  async function stop() {
    try { await cancelDuplicateScan(); notify.success('Duplicate scan stopped'); setScan((await getDuplicateScan()).job || null); }
    catch (err) { notify.error('stop the duplicate scan', err); }
  }
  async function dismiss(group) {
    try { await dismissDuplicate(group.id); notify.success('Marked as not duplicates'); await groups.reload(); }
    catch (err) { notify.error('dismiss this group', err); }
  }
  async function keep(group, mediaFileId) {
    const others = group.files.filter(f => !mediaFileId || f.mediaFileId !== mediaFileId).length - (mediaFileId ? 0 : 1);
    const ok = await confirm({
      title: mediaFileId ? 'Keep this file?' : 'Keep the best copy?',
      consequence: `${mediaFileId ? 'The other copies' : 'Every copy except the best one (the highest resolution and largest)'} are moved into a trash folder on the same disk. Nothing is deleted for good until you remove it from the trash on disk.`,
      affects: `${plural(Math.max(1, others), 'file')} in this group`, reversible: 'Yes. Restore them from the Trash list below, then scan the library.', confirmLabel: mediaFileId ? 'Keep this file' : 'Keep best copy', reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false },
    });
    if (!ok) return;
    try { const r = await keepDuplicate(group.id, mediaFileId, { reason: ok.reason }); notify.success(mediaFileId ? 'Kept that file' : 'Kept the best copy', `${r.trashed || 0} moved to trash. Restore them from the list below.`); await Promise.all([groups.reload(), trash.reload()]); }
    catch (err) { notify.error('move the other copies to trash', err); }
  }
  async function restore(item) {
    try { const r = await restoreTrash(item.id); notify.success('Restored', `${r.path} is back. Scan the library to index it again.`); await Promise.all([groups.reload(), trash.reload()]); }
    catch (err) { notify.error('restore that file', err); }
  }

  const list = groups.data || [];
  return <PageStack>
    <PageHeader title="Duplicates" subtitle="Library scans skip this. Start a duplicate scan to find exact copies, keep the best one, and move the others to a trash folder on the same disk."
      actions={running ? <Button variant="outline" onClick={stop} leadingIcon={<CircleStop size={14} />}>Stop scan</Button> : <Button onClick={start} disabled={starting} leadingIcon={<Copy size={14} />}>Find duplicates</Button>} />
    {scan && <div className="console-banner" data-tone={scan.status === 'FAILED' ? 'crit' : undefined} role="status">
      <Status tone={running ? 'accent' : scan.status === 'FAILED' ? 'crit' : 'neutral'} pulse={running}>{running ? 'Running' : scan.status === 'COMPLETED' ? 'Finished' : scan.status === 'FAILED' ? 'Failed' : 'Stopped'}</Status>
      <span className="console-banner__meta">{scanText(scan)}</span>
      {running && scan.total > 0 && <div className="console-banner__bar"><ProgressBar percent={Math.min(100, Math.round(((scan.hashed || 0) / scan.total) * 100))} label="Duplicate scan progress" /></div>}
    </div>}
    {libraries.length > 1 && <TableToolbar><LibraryScopeSelect /></TableToolbar>}

    {groups.error && !groups.data ? <Panel bodyless><ErrorState title="Couldn't load duplicates" error={groups.error} onRetry={groups.reload} /></Panel>
      : groups.loading ? <Panel bodyless><SkeletonRows rows={3} /></Panel>
        : list.length === 0 ? <Panel bodyless><EmptyState icon={Copy} title="No duplicates to review" message="Run Find duplicates to hash files and group exact copies." /></Panel>
          : list.map(group => <Panel key={group.id} title={`${group.kind} match${group.confidence ? ` · ${Math.round(group.confidence * 100)}% confidence` : ''}`} subtitle={plural(group.files.length, 'file')}
            actions={<><Button size="sm" onClick={() => keep(group)}>Keep best</Button><Button size="sm" variant="outline" onClick={() => dismiss(group)}>Not a duplicate</Button></>} bodyless>
            <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow><TableHead>Title</TableHead><TableHead>Storage</TableHead><TableHead>Path</TableHead><TableHead className="console-num-cell">Size</TableHead><TableHead>Resolution</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>{group.files.map(f => <TableRow key={f.mediaFileId}>
                <TableCell><a className="console-link" href={mediaHref(`/library/${f.mediaItemId}`)}>{f.mediaItemTitle}</a></TableCell>
                <TableCell className="console-nowrap">{f.storageLocation}</TableCell>
                <TableCell className="console-truncate-cell" title={f.relativePath}>{f.relativePath}</TableCell>
                <TableCell className="console-num-cell">{formatBytes(f.fileSize)}</TableCell>
                <TableCell className="console-nowrap">{f.width && f.height ? `${f.width}×${f.height}` : '—'}</TableCell>
                <TableCell><Button size="sm" variant="outline" onClick={() => keep(group, f.mediaFileId)}>Keep this</Button></TableCell>
              </TableRow>)}</TableBody>
            </Table></div>
          </Panel>)}

    {(trash.data || []).length > 0 && <Panel title="Trash" subtitle="Files moved aside. Restoring puts the file back; scan the library afterwards so it shows up again." bodyless>
      <ul className="console-rows">{trash.data.map(item => <li key={item.id} className="console-row">
        <div className="console-row__main"><span className="console-row__title">{item.title}</span><span className="console-row__meta">{item.path}</span></div>
        <div className="console-row__aside"><Button size="sm" variant="outline" onClick={() => restore(item)} leadingIcon={<Undo2 size={14} />}>Restore</Button></div>
      </li>)}</ul>
    </Panel>}
  </PageStack>;
}
