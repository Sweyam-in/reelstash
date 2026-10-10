import { LibraryScope, useAdminLibrary } from '../adminLibraryFilter';
import React, { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { mediaHref } from '../appLinks';
import { Badge, Button, EmptyState, Spinner, useToast } from '@spatika/react';
import { PageHeader } from '../adminHeader';
import { CircleStop, Copy } from 'lucide-react';
import { cancelDuplicateScan, dismissDuplicate, getDuplicateScan, getDuplicates, getTrash, keepDuplicate, restoreTrash, startDuplicateScan } from '../api';
import { useErrorToast } from '../hooks/useErrorToast';

function formatSize(bytes) {
  if (!bytes) return '—';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function fileName(path) {
  if (!path) return '';
  const parts = String(path).split(/[/\\]/);
  return parts[parts.length - 1] || path;
}

function scanStatusText(job) {
  if (!job) return '';
  if (job.status === 'PENDING') return 'Starting duplicate scan…';
  if (job.status === 'RUNNING' && job.phase === 'grouping') return 'Grouping duplicates…';
  if (job.status === 'RUNNING') {
    const current = fileName(job.currentPath);
    const counts = `${job.hashed || 0} / ${job.total || 0} hashed`;
    return current ? `Hashing files (${counts}) · ${current}` : `Hashing files (${counts})`;
  }
  if (job.status === 'FAILED') return job.error || 'Duplicate scan failed';
  if (job.status === 'CANCELLED') return 'Duplicate scan stopped';
  if (job.status === 'COMPLETED') {
    const failed = job.failed ? `, ${job.failed} failed` : '';
    return `Last duplicate scan finished. Hashed ${job.hashed || 0} of ${job.total || 0}${failed}.`;
  }
  return job.status;
}

export default function Duplicates({ admin = false }) {
  const adminLibrary = useAdminLibrary();
  const libraryId = admin ? adminLibrary.libraryId : null;
  const [groups, setGroups] = useState([]);
  const [trash, setTrash] = useState([]);
  const [loading, setLoading] = useState(true);
  const [scan, setScan] = useState(null);
  const [starting, setStarting] = useState(false);
  const notifyError = useErrorToast();
  const { toast } = useToast();
  const previousStatus = useRef(null);

  const load = (opts = {}) => {
    if (!opts.silent) setLoading(true);
    Promise.all([
      getDuplicates(libraryId ? { libraryId } : {}),
      admin ? getTrash().catch(() => ({ items: [] })) : Promise.resolve({ items: [] }),
    ]).then(([result, trashed]) => { setGroups(result.items || []); setTrash(trashed.items || []); }).catch(err => notifyError(err, 'Failed to load duplicates')).finally(() => setLoading(false));
  };
  useEffect(load, [libraryId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!admin) return undefined;
    let active = true;
    const tick = () => {
      getDuplicateScan()
        .then(result => { if (active) setScan(result.job || null); })
        .catch(err => { if (active) notifyError(err, 'Failed to load duplicate scan', { id: 'duplicate-scan-poll' }); });
    };
    tick();
    const interval = setInterval(tick, 2000);
    return () => { active = false; clearInterval(interval); };
  }, [admin]);

  useEffect(() => {
    const status = scan?.status || null;
    const wasRunning = previousStatus.current === 'PENDING' || previousStatus.current === 'RUNNING';
    if (wasRunning && status === 'COMPLETED') load({ silent: true });
    previousStatus.current = status;
  }, [scan]);

  const running = scan && (scan.status === 'PENDING' || scan.status === 'RUNNING');

  async function handleStart() {
    setStarting(true);
    try {
      await startDuplicateScan();
      toast({ title: 'Duplicate scan started', tone: 'success' });
      const result = await getDuplicateScan();
      setScan(result.job || null);
    } catch (err) {
      notifyError(err, 'Failed to start duplicate scan');
    } finally {
      setStarting(false);
    }
  }

  async function handleCancel() {
    try {
      await cancelDuplicateScan();
      toast({ title: 'Duplicate scan stopped', tone: 'success' });
      const result = await getDuplicateScan();
      setScan(result.job || null);
    } catch (err) {
      notifyError(err, 'Failed to stop duplicate scan');
    }
  }

  async function dismiss(id) {
    try { await dismissDuplicate(id); load({ silent: true }); }
    catch (err) { notifyError(err, 'Failed to dismiss duplicate'); }
  }

  async function keep(id, mediaFileId) {
    try {
      const result = await keepDuplicate(id, mediaFileId);
      toast({ title: mediaFileId ? 'Kept that file' : 'Kept the best copy', description: `${result.trashed || 0} moved to trash. Restore them from the list below.`, tone: 'success' });
      load({ silent: true });
    } catch (err) { notifyError(err, 'Could not move the other copies to trash'); }
  }

  async function restore(id) {
    try {
      const result = await restoreTrash(id);
      toast({ title: 'Restored', description: `${result.path} is back. Scan the library to index it again.`, tone: 'success' });
      load({ silent: true });
    } catch (err) { notifyError(err, 'Could not restore that file'); }
  }

  return <div className="reel-view">
    <PageHeader
      title="Duplicates"
      description={admin
        ? 'Library scans skip this. Start a duplicate scan when you want exact copies found. Keep the best copy to move the others into trash on the same disk. Nothing is deleted for good until you remove it from trash.'
        : 'Matching files found by an admin duplicate scan. Nothing is deleted automatically.'}
      actions={admin && (running
        ? <Button variant="danger" onClick={handleCancel}><CircleStop size={16} /> Stop scan</Button>
        : <Button onClick={handleStart} disabled={starting}><Copy size={16} /> Find duplicates</Button>)}
    />
    {admin && <LibraryScope />}

    {admin && scan && <p className="reel-state" style={{ paddingTop: 0 }}>
      {running && <Badge variant="default" style={{ marginRight: 8 }}>Running</Badge>}
      {scanStatusText(scan)}
    </p>}

    {loading ? <div className="reel-state"><Spinner /> Loading…</div> : groups.length === 0 ? (
      <EmptyState icon={Copy} title="No duplicates to review" description={admin ? 'Run Find duplicates to hash files and group exact copies.' : 'No matching files have been flagged.'} />
    ) : groups.map(group => <div key={group.id} className="reel-card" style={{ display: 'block', padding: '1rem', marginBottom: '1rem' }}>
      <div className="rs-dupe-head">
        <strong><Copy size={16} style={{ verticalAlign: 'middle', marginRight: 6 }} />{group.kind} match{group.confidence ? ` · ${Math.round(group.confidence * 100)}% confidence` : ''}</strong>
        <span>
          {admin && <Button size="sm" onClick={() => keep(group.id)}>Keep best</Button>}
          <Button size="sm" variant="secondary" onClick={() => dismiss(group.id)}>Not a duplicate</Button>
        </span>
      </div>
      <div className="rs-dupe-table"><table>
        <thead><tr><th style={{ textAlign: 'left' }}>Title</th><th style={{ textAlign: 'left' }}>Storage</th><th style={{ textAlign: 'left' }}>Path</th><th style={{ textAlign: 'left' }}>Size</th><th style={{ textAlign: 'left' }}>Resolution</th>{admin && <th />}</tr></thead>
        <tbody>{group.files.map(f => <tr key={f.mediaFileId}>
          <td>{admin ? <a href={mediaHref(`/library/${f.mediaItemId}`)}>{f.mediaItemTitle}</a> : <Link to={`/library/${f.mediaItemId}`}>{f.mediaItemTitle}</Link>}</td>
          <td>{f.storageLocation}</td>
          <td>{f.relativePath}</td>
          <td>{formatSize(f.fileSize)}</td>
          <td>{f.width && f.height ? `${f.width}x${f.height}` : '—'}</td>
          {admin && <td><Button size="sm" variant="secondary" onClick={() => keep(group.id, f.mediaFileId)}>Keep this</Button></td>}
        </tr>)}</tbody>
      </table></div>
    </div>)}
    {admin && trash.length > 0 && <section className="reel-card" style={{ display: 'block', padding: '1rem', marginTop: '1rem' }}>
      <strong>Trash</strong>
      <p>These files were moved aside. Restoring puts the file back; scan the library afterwards so it shows up again.</p>
      <ul>{trash.map(item => <li key={item.id}>{item.title} · {item.path} <Button size="sm" variant="secondary" onClick={() => restore(item.id)}>Restore</Button></li>)}</ul>
    </section>}
  </div>;
}
