import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Button } from '@spatika/react';
import { Clapperboard, LibraryBig, Lock, Plus, RefreshCw, TriangleAlert } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { cancelLibraryScan, createLibrary, getAdminProviders, getLibraries, getStorageLocations, getUsers, startLibraryScan } from '../../api';
import { modeTerms, LIBRARY_MODES } from '../../libraryModes';
import { Callout, EmptyState, ErrorState, FormDialog, MetricStrip, PageHeader, PageStack, Panel, SkeletonRows, Status, Tag, TableToolbar, rowActivation } from '../components';
import { formatBytes, formatNumber, plural, timeAgo } from '../lib/format';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { EMPTY_LIBRARY, LibraryForm, rootLabel } from './libraries/LibraryForm';

export function LibrariesPage() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const notify = useNotify();
  const libs = useResource(() => getLibraries(), [], { interval: 0 });
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), []);
  const providers = useResource(() => getAdminProviders().then(r => r.items || []), []);
  const users = useResource(() => getUsers().then(r => r.items || []), []);
  const [creating, setCreating] = useState(false);
  const [form, setForm] = useState(EMPTY_LIBRARY);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [scanBusy, setScanBusy] = useState(null);
  const [q, setQ] = useState(params.get('q') || '');

  useEffect(() => { if (params.get('new') === '1') { openCreate(); setParams(prev => { const n = new URLSearchParams(prev); n.delete('new'); return n; }, { replace: true }); } }, [params]); // eslint-disable-line react-hooks/exhaustive-deps
  const data = libs.data;
  const items = data?.items || [];
  // Poll while a library is scanning so its status and counts stay current.
  const scanning = items.some(l => l.scanning);
  useEffect(() => {
    if (!scanning) return undefined;
    const t = setInterval(libs.reload, 4000);
    return () => clearInterval(t);
  }, [scanning]); // eslint-disable-line react-hooks/exhaustive-deps

  function openCreate() { setForm(EMPTY_LIBRARY); setError(null); setCreating(true); }

  async function create() {
    setBusy(true); setError(null);
    try {
      await createLibrary({ name: form.name.trim(), mode: form.mode, defaultAccess: form.defaultAccess, roots: form.roots, providers: form.providers, userIds: form.userIds });
      notify.success('Library created', form.roots.length ? 'Titles in its folders now belong to it.' : 'Add folders to fill it.');
      setCreating(false);
      await libs.reload();
    } catch (err) { setError(describeError('create the library', err)); }
    finally { setBusy(false); }
  }

  async function scan(library, stop) {
    setScanBusy(library.id);
    try { await (stop ? cancelLibraryScan(library.id) : startLibraryScan(library.id)); notify.success(stop ? 'Scan stopped' : `Scanning ${library.name}`); await libs.reload(); }
    catch (err) { notify.error(stop ? 'stop the scan' : 'start the scan', err); }
    finally { setScanBusy(null); }
  }

  const diskList = disks.data || [];
  const uncovered = useMemo(() => diskList.filter(d => !items.some(l => l.roots.some(r => r.storageLocationId === d.id))), [diskList, items]);
  const needle = q.trim().toLowerCase();
  const rows = items.filter(l => !needle || l.name.toLowerCase().includes(needle));

  return <PageStack>
    <PageHeader title="Libraries" subtitle="Group folders into libraries, choose how each is presented and matched, and who can open it."
      actions={<Button onClick={openCreate} leadingIcon={<Plus size={14} />}>New library</Button>} />
    <MetricStrip label="Library summary" loading={libs.loading} items={[
      { label: 'Libraries', value: formatNumber(items.length) },
      { label: 'Titles', value: formatNumber(items.reduce((t, l) => t + l.mediaCount, 0)) },
      { label: 'Size', value: formatBytes(items.reduce((t, l) => t + l.totalBytes, 0)) },
      { label: 'Outside any library', value: formatNumber(data?.unassignedCount ?? 0), detail: 'Only admins see them', tone: data?.unassignedCount > 0 ? 'warning' : undefined },
    ]} />
    {data && data.unassignedCount > 0 && <Callout tone="warn" icon={<TriangleAlert />} title={`${plural(data.unassignedCount, 'title is', 'titles are')} outside every library`}>
      Only admins see them{uncovered.length ? `. Disks not in any library: ${uncovered.map(d => d.name).join(', ')}.` : '. Add their folders to a library to share them.'}
    </Callout>}

    <Panel bodyless>
      <TableToolbar search={q} onSearch={setQ} label="Search libraries" placeholder="Search libraries" />
      {libs.error && !data ? <ErrorState title="Couldn't load libraries" error={libs.error} onRetry={libs.reload} />
        : libs.loading ? <SkeletonRows rows={3} label="Loading libraries" />
          : rows.length === 0 ? <EmptyState icon={LibraryBig} title={items.length ? 'No library matches' : 'No libraries yet'} message={items.length ? 'Try another name.' : 'Create one and add the folders it should show. Until then only admins see your titles.'} action={!items.length && <Button size="sm" onClick={openCreate}>New library</Button>} />
            : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow><TableHead>Library</TableHead><TableHead>Folders</TableHead><TableHead className="console-num-cell">Titles</TableHead><TableHead className="console-num-cell">Size</TableHead><TableHead>Providers</TableHead><TableHead>Access</TableHead><TableHead>Last scan</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>
                {rows.map(library => {
                  const terms = modeTerms(library.mode);
                  const mode = LIBRARY_MODES.find(m => m.id === library.mode);
                  const first = library.roots[0] && rootLabel(library.roots[0], diskList);
                  return <TableRow key={library.id} {...rowActivation(() => navigate(`/admin/libraries/${library.id}`))} aria-label={`${library.name}, open details`}>
                    <TableCell><span className="console-title-cell">{library.mode === 'private' ? <Lock size={14} aria-hidden="true" /> : <Clapperboard size={14} aria-hidden="true" />}<span className="console-title-cell__copy"><span className="console-title-cell__name">{library.name}</span><span className="console-title-cell__sub">{mode?.label}</span></span></span></TableCell>
                    <TableCell className="console-muted">{first ? <>{first.disk} <code className="console-code">{first.path}</code>{library.roots.length > 1 && ` +${library.roots.length - 1}`}</> : 'None yet'}</TableCell>
                    <TableCell className="console-num-cell" title={terms.titles}>{formatNumber(library.mediaCount)}</TableCell>
                    <TableCell className="console-num-cell">{formatBytes(library.totalBytes)}</TableCell>
                    <TableCell className="console-muted">{library.providers.length ? library.providers.join(' → ') : 'Matching off'}</TableCell>
                    <TableCell className="console-muted console-nowrap">{plural(library.userIds.length, 'user')}{library.defaultAccess ? ' + new' : ''}</TableCell>
                    <TableCell className="console-nowrap">{library.scanning ? <Status tone="accent" pulse>Scanning</Status> : <span className="console-muted">{library.lastScanAt ? timeAgo(library.lastScanAt) : 'Never'}</span>}</TableCell>
                    <TableCell>{library.scanning
                      ? <Button size="sm" variant="ghost" disabled={scanBusy === library.id} onClick={() => scan(library, true)}>Stop scan</Button>
                      : <Button size="sm" variant="ghost" disabled={scanBusy === library.id || !library.roots.length} onClick={() => scan(library, false)} leadingIcon={<RefreshCw size={14} />} aria-label={`Scan ${library.name}`}>Scan</Button>}</TableCell>
                  </TableRow>;
                })}
              </TableBody>
            </Table></div>}
    </Panel>

    <FormDialog open={creating} onOpenChange={setCreating} title="New library" description="A library is a set of folders on your disks that people browse together." size="lg"
      submitLabel="Create library" dirty={JSON.stringify(form) !== JSON.stringify(EMPTY_LIBRARY)} canSubmit={!!form.name.trim()} submitting={busy} error={error} onSubmit={create}>
      <LibraryForm isNew form={form} setForm={setForm} disks={diskList} providers={providers.data || []} users={users.data || []} />
    </FormDialog>
  </PageStack>;
}
