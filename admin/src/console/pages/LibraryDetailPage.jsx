import React, { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Button } from '@spatika/react';
import { CircleStop, Clapperboard, LibraryBig, Lock, RefreshCw, Trash2 } from 'lucide-react';
import { cancelLibraryScan, deleteLibrary, getAdminProviders, getLibraries, getStorageLocations, getUsers, startLibraryScan, updateLibrary } from '../../api';
import { LIBRARY_MODES, modeTerms } from '../../libraryModes';
import { CopyButton, DetailList, EmptyState, ErrorState, MoreMenu, PageHeader, PageStack, Panel, ProfileHeader, SegmentedTabs, SettingsGroup, SettingsRow, SkeletonRows, Status, Tag, useConfirm, MetricStrip } from '../components';
import { formatBytes, formatDateTime, formatNumber, plural, timeAgo, userLabel } from '../lib/format';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';
import { useCrumb } from '../shell/Breadcrumbs';
import { LibraryForm, rootLabel } from './libraries/LibraryForm';

const toForm = library => ({ name: library.name, mode: library.mode, defaultAccess: library.defaultAccess, roots: library.roots, providers: library.providers, userIds: library.userIds });

export function LibraryDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const notify = useNotify();
  const confirm = useConfirm();
  const [filters, setFilters] = useUrlState({ tab: 'overview' });
  const libs = useResource(() => getLibraries(), [], { interval: 0 });
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), []);
  const providers = useResource(() => getAdminProviders().then(r => r.items || []), []);
  const users = useResource(() => getUsers().then(r => r.items || []), []);
  const library = (libs.data?.items || []).find(l => String(l.id) === id);
  useCrumb(library ? library.name : libs.error ? 'Not found' : null);

  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (library) setForm(toForm(library)); }, [library?.id, library && JSON.stringify(toForm(library))]); // eslint-disable-line react-hooks/exhaustive-deps
  const dirty = !!library && !!form && JSON.stringify(form) !== JSON.stringify(toForm(library));
  useEffect(() => {
    if (!dirty) return undefined;
    const warn = e => { e.preventDefault(); e.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);
  useEffect(() => {
    if (!library?.scanning) return undefined;
    const t = setInterval(libs.reload, 4000);
    return () => clearInterval(t);
  }, [library?.scanning]); // eslint-disable-line react-hooks/exhaustive-deps

  const diskList = disks.data || [];
  if (libs.error && !libs.data) return <PageStack><PageHeader title="Library" back={{ to: '/admin/libraries', label: 'All libraries' }} /><Panel bodyless><ErrorState title="Couldn't load this library" error={libs.error} onRetry={libs.reload} /></Panel></PageStack>;
  if (!libs.data) return <PageStack><Panel bodyless><SkeletonRows rows={4} label="Loading library" /></Panel></PageStack>;
  if (!library) return <PageStack><PageHeader title="Library not found" back={{ to: '/admin/libraries', label: 'All libraries' }} /><Panel bodyless><EmptyState icon={LibraryBig} title="There is no library with this ID" message="It may have been deleted, or the link is wrong." /></Panel></PageStack>;

  const terms = modeTerms(library.mode);
  const mode = LIBRARY_MODES.find(m => m.id === library.mode);
  const members = (users.data || []).filter(u => library.userIds.includes(u.id));

  async function scan(stop) {
    setBusy(true);
    try { await (stop ? cancelLibraryScan(library.id) : startLibraryScan(library.id)); notify.success(stop ? 'Scan stopped' : `Scanning ${library.name}`); await libs.reload(); }
    catch (err) { notify.error(stop ? 'stop the scan' : 'start the scan', err); }
    finally { setBusy(false); }
  }

  async function save() {
    setBusy(true); setError(null);
    try {
      await updateLibrary(library.id, { name: form.name.trim(), mode: form.mode, defaultAccess: form.defaultAccess, roots: form.roots, providers: form.providers, userIds: form.userIds });
      notify.success('Library saved', form.roots.length ? 'Titles in its folders now belong to it.' : 'Add folders to fill it.');
      await libs.reload();
    } catch (err) { setError(describeError('save the library', err)); }
    finally { setBusy(false); }
  }

  async function remove() {
    const ok = await confirm({
      title: `Delete the library ${library.name}?`,
      consequence: 'No files or titles are deleted. Its titles become visible to admins only until another library covers their folders, and the people who could open it lose access.',
      affects: `${plural(library.mediaCount, 'title')} and ${plural(library.userIds.length, 'user')} with access`, reversible: 'Partly. You can create the library again, but its settings and access list are not kept.',
      confirmLabel: 'Delete library', tone: 'danger', typeToConfirm: library.name, reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.' },
    });
    if (!ok) return;
    try { await deleteLibrary(library.id, { reason: ok.reason }); notify.success('Library deleted', 'Its titles stay indexed and can be added to another library.'); navigate('/admin/libraries'); }
    catch (err) { notify.error('delete the library', err); }
  }

  const switchTab = async tab => {
    if (dirty) {
      const ok = await confirm({ title: 'Discard unsaved changes?', consequence: 'The settings you changed have not been saved and will be lost.', confirmLabel: 'Discard changes', tone: 'danger' });
      if (!ok) return;
      setForm(toForm(library));
    }
    setFilters({ tab });
  };

  return <PageStack>
    <ProfileHeader
      mark={library.mode === 'private' ? <Lock size={22} aria-hidden="true" /> : <Clapperboard size={22} aria-hidden="true" />}
      title={library.name}
      status={library.scanning ? <Status tone="accent" pulse>Scanning now</Status> : <Status tone="ok">Idle</Status>}
      meta={<><span>{mode?.label}</span><span>{plural(library.mediaCount, terms.title.toLowerCase())} · {formatBytes(library.totalBytes)}</span><CopyButton value={String(library.id)} label="Library ID" display={`ID ${library.id}`} /></>}
      actions={<>
        {library.scanning ? <Button variant="outline" disabled={busy} onClick={() => scan(true)} leadingIcon={<CircleStop size={14} />}>Stop scan</Button>
          : <Button disabled={busy || !library.roots.length} title={library.roots.length ? undefined : 'Add a folder first'} onClick={() => scan(false)} leadingIcon={<RefreshCw size={14} />}>Scan now</Button>}
        <MoreMenu items={[{ label: 'Delete library…', icon: Trash2, danger: true, onSelect: remove }]} />
      </>} />

    <SegmentedTabs page label="Library sections" value={filters.tab} onChange={switchTab} options={[{ value: 'overview', label: 'Overview' }, { value: 'settings', label: 'Folders & access' }, { value: 'danger', label: 'Danger zone' }]} />

    {filters.tab === 'overview' && <>
      <MetricStrip label="Library figures" items={[
        { label: terms.titles, value: formatNumber(library.mediaCount) },
        { label: 'Size', value: formatBytes(library.totalBytes) },
        { label: 'Folders', value: formatNumber(library.roots.length) },
        { label: 'Last scan', value: library.scanning ? 'Now' : library.lastScanAt ? timeAgo(library.lastScanAt) : 'Never', detail: library.lastScanAt ? formatDateTime(library.lastScanAt) : undefined },
      ]} />
      <Panel title="Configuration" bodyless>
        <DetailList flush items={[
          { term: 'Mode', details: <><Tag>{mode?.label}</Tag> <span className="console-muted console-small">{mode?.description}</span></> },
          { term: 'Folders', details: library.roots.length ? <ul className="console-plain-list">{library.roots.map(root => { const l = rootLabel(root, diskList); return <li key={`${root.storageLocationId}:${root.path}`}>{l.disk} <code className="console-code">{l.path}</code></li>; })}</ul> : <span className="console-muted">None yet</span> },
          { term: 'Providers', details: library.providers.length ? library.providers.map(pid => (providers.data || []).find(p => p.id === pid)?.name || pid).join(' → ') : <span className="console-muted">Matching off</span> },
          { term: 'Access', details: <>{plural(library.userIds.length, 'user')}{library.defaultAccess ? ' · new users included' : ''}{members.length > 0 && <span className="console-muted"> ({members.map(userLabel).join(', ')})</span>}</> },
        ]} />
      </Panel>
    </>}

    {filters.tab === 'settings' && <Panel title="Folders, matching and access" subtitle="Save applies everything below at once."
      footer={<><span>{dirty ? 'Unsaved changes' : 'Saved'}</span><span><Button size="sm" variant="ghost" disabled={!dirty || busy} onClick={() => setForm(toForm(library))}>Discard</Button> <Button size="sm" disabled={!dirty || !form?.name.trim()} loading={busy} onClick={save}>Save changes</Button></span></>}>
      {error && <p className="console-field__error" role="alert">{error} What you changed is still here.</p>}
      {form && <LibraryForm form={form} setForm={setForm} disks={diskList} providers={providers.data || []} users={users.data || []} />}
    </Panel>}

    {filters.tab === 'danger' && <SettingsGroup danger title="Danger zone" description="No files on disk are ever touched.">
      <SettingsRow label="Delete this library" description="Its titles stay indexed but only admins see them until another library covers their folders."><Button size="sm" variant="destructive" onClick={remove}>Delete library…</Button></SettingsRow>
    </SettingsGroup>}
  </PageStack>;
}
