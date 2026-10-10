import React, { useEffect, useState } from 'react';
import { Button } from '@spatika/react';
import { Clapperboard, Sparkles } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { applyRename, getBackgroundJob, getRenameQueue, namerMatch, namerSelect } from '../../api';
import { BulkBar, DetailDrawer, EmptyState, ErrorState, PageHeader, PageStack, Panel, SectionTabs, SkeletonRows, Status, TablePagination, TableToolbar, useConfirm } from '../components';
import { plural } from '../lib/format';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { tabsOf } from '../lib/navigation';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

const PAGE = 50;

async function waitForNamerJob(jobId) {
  for (;;) {
    const job = await getBackgroundJob(jobId);
    if (job.status === 'COMPLETED') return job.result || {};
    if (job.status === 'FAILED' || job.status === 'CANCELLED') throw new Error(job.error || `Match ${job.status.toLowerCase()}`);
    await new Promise(resolve => setTimeout(resolve, 2000));
  }
}

export function RenamePage() {
  const admin = useAdminLibrary();
  const notify = useNotify();
  const confirm = useConfirm();
  const queue = useResource(() => getRenameQueue(admin.libraryId ? { libraryId: admin.libraryId } : {}), [admin.libraryId]);
  const [busyId, setBusyId] = useState(null);
  const [selected, setSelected] = useState([]);
  const [candidates, setCandidates] = useState(null); // { item, items }
  const [page, setPage] = useState(0);
  const items = queue.data?.items || [];
  const template = queue.data?.template || '';
  useEffect(() => { setSelected([]); setPage(0); }, [admin.libraryId]);
  const paged = items.slice(page * PAGE, page * PAGE + PAGE);

  async function apply(list) {
    const ok = await confirm({
      title: list.length === 1 ? `Rename “${list[0].oldFileName}”?` : `Rename ${plural(list.length, 'file')}?`,
      consequence: list.length === 1 ? `The file on disk becomes “${list[0].newFileName}”, following the naming template. The library is updated, so watch history and ratings are kept.` : 'Each file on disk is renamed to its proposed name, following the naming template. The library is updated, so watch history and ratings are kept.',
      affects: plural(list.length, 'file'), reversible: 'Partly. The old name is not remembered; rename it again to change it back.', confirmLabel: list.length === 1 ? 'Rename file' : `Rename ${list.length} files`,
    });
    if (!ok) return;
    let done = 0; let failed = 0; let firstError = null;
    for (const item of list) {
      setBusyId(item.mediaFileId);
      try { await applyRename(item.mediaFileId); done += 1; } catch (err) { failed += 1; firstError ||= err; }
    }
    setBusyId(null); setSelected([]);
    if (failed) notify.error(`rename ${plural(failed, 'file')}`, firstError); else notify.success(`Renamed ${plural(done, 'file')}`);
    await queue.reload();
  }

  async function match(item) {
    setBusyId(item.mediaFileId);
    try {
      const queued = await namerMatch(item.mediaFileId);
      const result = await waitForNamerJob(queued.backgroundJobId);
      if (result.status === 'matched') { notify.success('Matched', 'The file was renamed from the provider’s details.'); await queue.reload(); }
      else if (result.status === 'manual_required') setCandidates({ item, items: result.candidates || [] });
      else if (result.status === 'no_match') notify.info('No match found', 'No provider found a match for this file. Check its name, or match it by hand from Videos.');
    } catch (err) { notify.error('match this file', err); }
    finally { setBusyId(null); }
  }

  async function choose(candidate) {
    const { item } = candidates;
    setCandidates(null);
    setBusyId(item.mediaFileId);
    try { const queued = await namerSelect(item.mediaFileId, candidate.provider, candidate.externalId || candidate.uuid); await waitForNamerJob(queued.backgroundJobId); notify.success('Match applied'); await queue.reload(); }
    catch (err) { notify.error('apply the match', err); }
    finally { setBusyId(null); }
  }

  return <PageStack>
    <PageHeader title="Rename pipeline" subtitle={<>Files whose names don’t follow the template{admin.library ? ` for ${admin.library.name}` : ''}: <code className="console-code">{template || '…'}</code></>} />
    <SectionTabs entries={tabsOf('organizer')} activePath="/admin/rename" label="Organizer sections" />
    <Panel bodyless>
      <TableToolbar>{admin.libraries.length > 1 && <LibraryScopeSelect />}</TableToolbar>
      <BulkBar count={selected.length} noun={selected.length === 1 ? 'file' : 'files'} onClear={() => setSelected([])}>
        <Button size="sm" onClick={() => apply(items.filter(i => selected.includes(i.mediaFileId)))}>Apply template to selected…</Button>
      </BulkBar>
      {queue.error && !queue.data ? <ErrorState title="Couldn't load the rename queue" error={queue.error} onRetry={queue.reload} />
        : queue.loading ? <SkeletonRows rows={5} />
          : items.length === 0 ? <EmptyState icon={Clapperboard} title="Nothing to rename" message="Every indexed file already matches the naming template." />
            : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow>
                <TableHead className="console-table-check"><input type="checkbox" aria-label="Select all files on this page" checked={paged.length > 0 && paged.every(i => selected.includes(i.mediaFileId))} onChange={e => setSelected(e.target.checked ? paged.map(i => i.mediaFileId) : [])} /></TableHead>
                <TableHead>Current name</TableHead><TableHead>Proposed name</TableHead><TableHead>Storage</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead>
              </TableRow></TableHeader>
              <TableBody>{paged.map(item => <TableRow key={item.mediaFileId} data-selected={selected.includes(item.mediaFileId) || undefined}>
                <TableCell className="console-table-check"><input type="checkbox" aria-label={`Select ${item.oldFileName}`} checked={selected.includes(item.mediaFileId)} onChange={e => setSelected(s => e.target.checked ? [...s, item.mediaFileId] : s.filter(x => x !== item.mediaFileId))} /></TableCell>
                <TableCell className="console-truncate-cell" title={item.oldFileName}>{item.oldFileName}</TableCell>
                <TableCell className="console-truncate-cell" title={item.newFileName}>{item.newFileName}</TableCell>
                <TableCell className="console-nowrap">{item.storageLocation}</TableCell>
                <TableCell><span className="console-nowrap">
                  <Button size="sm" variant="outline" disabled={busyId === item.mediaFileId} onClick={() => apply([item])}>Apply template…</Button>
                  <Button size="sm" variant="ghost" disabled={busyId === item.mediaFileId} onClick={() => match(item)} leadingIcon={<Sparkles size={14} />}>{busyId === item.mediaFileId ? 'Working…' : 'Match with providers'}</Button>
                </span></TableCell>
              </TableRow>)}</TableBody>
            </Table></div>}
      {items.length > PAGE && <TablePagination limit={PAGE} offset={page * PAGE} total={items.length} noun="files" pageSizes={null} onChange={({ offset }) => setPage(offset / PAGE)} />}
    </Panel>
    <DetailDrawer open={!!candidates} onClose={() => setCandidates(null)} eyebrow="Several matches" title="Choose a match" description={candidates ? `For ${candidates.item.oldFileName}. The file is renamed from the one you pick.` : undefined}>
      {candidates?.items.length === 0 && <p className="console-muted console-small">No candidates were returned.</p>}
      <ul className="console-candidates console-candidates--drawer">
        {(candidates?.items || []).map(c => <li key={`${c.provider}:${c.externalId || c.uuid}`} className="console-candidate">
          <div className="console-candidate__main"><span className="console-candidate__title">{c.title}</span><span className="console-row__meta">{[c.site, c.date, (c.performers || []).join(', ')].filter(Boolean).join(' · ')}</span></div>
          <div className="console-candidate__flags">{c.provider && <span className="console-tag">{c.provider}</span>}<Status tone={c.score >= 90 ? 'ok' : 'neutral'}>{Math.round(c.score || 0)}%</Status>{c.siteMatch && <span className="console-tag" data-tone="accent">Studio</span>}{c.dateMatch && <span className="console-tag" data-tone="accent">Date</span>}</div>
          <Button size="sm" onClick={() => choose(c)}>Use this</Button>
        </li>)}
      </ul>
    </DetailDrawer>
  </PageStack>;
}
