import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Input } from '@spatika/react';
import { Copy, Eraser, Pencil, Plus, RotateCcw, Settings2, Sparkles, Tags, Trash2, X } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import {
  cleanupCategories, cleanupPerformers, createCategory, createPerformer, createSeries, createStudio, createTag, deleteCategory, deletePerformer, deleteSeries, deleteStudio, deleteTag,
  dismissPerformerDuplicate, getBackgroundJob, getCategories, getEmptyCategories, getEmptyPerformers, getPerformerDuplicates, getPerformerSync, getPerformers, getSeries, getStudioSync,
  getStudios, getTags, performerImageUrl, startPerformerSync, startStudioSync, studioImageUrl, updateCategory, updatePerformer, updateSeries, updateStudio, updateTag,
} from '../../api';
import { mediaHref } from '../../appLinks';
import { monogram } from '../../components/Monogram';
import PerformerSettings, { DuplicateList, NewPerformerDialog } from '../../pages/PerformerSettings';
import StudioSettings from '../../pages/StudioSettings';
import {
  EmptyState, ErrorState, Field, FormDialog, MoreMenu, PageHeader, PageStack, Panel, SectionTabs, SegmentedTabs, SkeletonRows, Status, TablePagination, TableToolbar, useConfirm,
} from '../components';
import { describeError } from '../lib/errors';
import { formatNumber, plural, timeAgo } from '../lib/format';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { tabsOf } from '../lib/navigation';
import { useNotify } from '../lib/notify';
import { useDebouncedValue } from '../lib/useDebouncedValue';
import { useUrlState } from '../lib/useUrlState';

// Admin › Metadata. Every page has the same shape: a header with the main actions, one match-run line
// when the entity has providers, then one toolbar (status filter, search) over one table.

const MATCH_STATUSES = [['ambiguous', 'Choose match', 'warn'], ['unmatched', 'Not checked', 'neutral'], ['no_match', 'No match', 'neutral'], ['matched', 'Matched', 'ok']];
const MATCH_LABEL = Object.fromEntries(MATCH_STATUSES.map(([v, l]) => [v, l]));
const MATCH_TONE = Object.fromEntries(MATCH_STATUSES.map(([v, , t]) => [v, t]));
const PAGE_DEFAULT = 50;
const metadataTabs = tabsOf('metadata');

function syncSummary(job) {
  const r = job?.result;
  if (!r) return '';
  const parts = [`${(r.processed || 0).toLocaleString()} of ${(r.total || 0).toLocaleString()} checked`];
  if (r.matched) parts.push(`${r.matched} matched`);
  if (r.ambiguous) parts.push(`${r.ambiguous} need a choice`);
  if (r.noMatch) parts.push(`${r.noMatch} with no match`);
  return parts.join(' · ');
}

// The latest provider-match job for one entity kind, polled while it runs.
function useMatchJob({ start, status, onFinished }) {
  const notify = useNotify();
  const [job, setJob] = useState(null);
  const [busy, setBusy] = useState(false);
  const running = !!job && (job.status === 'PENDING' || job.status === 'RUNNING');
  useEffect(() => { let live = true; status().then(r => { if (live) setJob(r.job); }).catch(() => {}); return () => { live = false; }; }, [status]);
  useEffect(() => {
    if (!running || !job?.id) return undefined;
    const t = setTimeout(() => { getBackgroundJob(job.id).then(next => { setJob(next); if (next.status === 'COMPLETED' || next.status === 'FAILED') onFinished?.(); }).catch(() => {}); }, 2000);
    return () => clearTimeout(t);
  }, [running, job, onFinished]);
  async function run(retry = false) {
    setBusy(true);
    try { const queued = await start(retry); setJob(await getBackgroundJob(queued.backgroundJobId)); }
    catch (err) { notify.error('start matching', err); }
    finally { setBusy(false); }
  }
  return { job, running, busy, run };
}

function MatchRunBar({ job, running, noun }) {
  const [dismissed, setDismissed] = useState(null);
  if (!job || dismissed === job.id) return null;
  const result = job.result || {};
  const failed = job.status === 'FAILED' || (!job.result && job.error);
  const when = job.finishedAt || job.createdAt;
  let headline;
  if (running) headline = `Matching ${noun} with metadata providers`;
  else if (failed) headline = `The last match run ${job.status === 'FAILED' ? 'failed' : 'stopped'}${when ? ` ${timeAgo(when)}` : ''}`;
  else headline = `Last match run${when ? ` ${timeAgo(when)}` : ''}`;
  const detail = failed ? job.error : [syncSummary(job), running && result.current ? `now ${result.current}` : '', result.stopped].filter(Boolean).join(' · ');
  const tone = running ? 'accent' : failed || result.stopped ? 'warn' : 'ok';
  return <div className="console-banner" data-tone={failed ? 'crit' : result.stopped ? 'warn' : undefined} role="status">
    <Status tone={tone} pulse={running}><span className="console-banner__title">{headline}</span></Status>
    {detail && <span className="console-banner__meta">{detail}</span>}
    {!running && <Button variant="ghost" size="icon-sm" className="console-banner__link" aria-label="Hide" onClick={() => setDismissed(job.id)}><X size={14} /></Button>}
  </div>;
}

function RowAvatar({ kind, item }) {
  const [failed, setFailed] = useState(false);
  const imageId = kind === 'studio' ? item.logoImageId : item.profileImageId;
  const src = imageId ? (kind === 'studio' ? studioImageUrl(item.id, imageId) : performerImageUrl(item.id, imageId)) : null;
  if (src && !failed) return <span className="console-avatar-img"><img src={src} alt="" loading="lazy" onError={() => setFailed(true)} /></span>;
  const mono = monogram(item.name);
  return <span className="console-avatar-img console-avatar-mono" style={mono.style} aria-hidden="true">{mono.initials}</span>;
}

function usePerformerCounts({ enabled, libraryId, query, revision }) {
  const [counts, setCounts] = useState(null);
  useEffect(() => {
    if (!enabled) return undefined;
    let live = true;
    const base = { limit: 1, ...(query ? { q: query } : {}), ...(libraryId ? { library: libraryId } : {}) };
    Promise.all(MATCH_STATUSES.map(([status]) => getPerformers({ ...base, matchStatus: status }).then(r => [status, r.total || 0])))
      .then(pairs => { if (live) setCounts(Object.fromEntries(pairs)); }).catch(() => { if (live) setCounts(null); });
    return () => { live = false; };
  }, [enabled, libraryId, query, revision]);
  return counts;
}

// A deliberate delete: the record goes, the titles stay but lose the link. Typed confirmation once titles are linked.
function useDeleteEntity(entityLabel, remove, onDone) {
  const confirm = useConfirm();
  const notify = useNotify();
  return async item => {
    const linked = item.mediaCount ?? 0;
    const ok = await confirm({
      title: `Delete ${item.name}?`, consequence: `Removes the ${entityLabel.toLowerCase()} record. Titles stay in the library, just unlinked from it.`,
      affects: linked ? plural(linked, 'linked title') : 'No titles are linked', reversible: false, confirmLabel: `Delete ${entityLabel.toLowerCase()}`, tone: 'danger', typeToConfirm: linked > 0 ? item.name : undefined,
    });
    if (!ok) return;
    try { await remove(item.id); notify.success(`${entityLabel} deleted`); onDone(); }
    catch (err) { notify.error(`delete ${entityLabel.toLowerCase()}`, err); }
  };
}

function EntityManager({
  entityLabel, entityPlural, list, create, update, remove, showMatch, linkPrefix = '/performers', avatar = null, revision = 0, onChanged, performers = false,
  libraryId = null, libraryScoped = true, addRef, tools = [], notice = null, onTotal, addInToolbar = false, storageKey,
}) {
  const studios = avatar === 'studio';
  const notify = useNotify();
  const [filters, setFilters] = useUrlState({ q: '', status: '', page: '0', size: String(PAGE_DEFAULT) });
  const [search, setSearch] = useState(filters.q);
  const query = useDebouncedValue(search, 250).trim();
  const [items, setItems] = useState(null);
  const [error, setError] = useState(null);
  const [total, setTotal] = useState(0);
  const [editing, setEditing] = useState(null); // {} = create, {id,...} = edit
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const [formError, setFormError] = useState(null);
  const [settingsId, setSettingsId] = useState(null);
  const [creatingPerformer, setCreatingPerformer] = useState(false);
  const [localRevision, setLocalRevision] = useState(0);
  const noun = entityPlural.toLowerCase();
  const size = Number(filters.size) || PAGE_DEFAULT;
  const page = Number(filters.page) || 0;
  const status = filters.status;
  const serverCounts = usePerformerCounts({ enabled: performers, libraryId: libraryScoped ? libraryId : null, query, revision: revision + localRevision });
  useEffect(() => { if (query !== filters.q) setFilters({ q: query }); }, [query]); // eslint-disable-line react-hooks/exhaustive-deps

  // People can number in the thousands, so they page and search on the server; the other lists are small
  // enough to load whole and filter here.
  const load = useCallback(() => {
    const params = performers ? { limit: size, offset: page * size, q: query, matchStatus: status } : { limit: 500 };
    if (libraryScoped && libraryId) params.library = libraryId;
    Object.keys(params).forEach(key => { if (params[key] === '') delete params[key]; });
    return list(params).then(result => { setError(null); setTotal(result.total ?? (result.items || []).length); setItems(result.items || []); }).catch(err => { setError(err); setItems(cur => cur || []); });
  }, [performers, size, page, query, status, libraryId, libraryScoped, list]);
  useEffect(() => { load(); }, [load, revision, localRevision]);
  useEffect(() => { onTotal?.(total); }, [total]); // eslint-disable-line react-hooks/exhaustive-deps
  const changed = useCallback(() => { setLocalRevision(v => v + 1); onChanged?.(); }, [onChanged]);
  const askDelete = useDeleteEntity(entityLabel, remove, changed);

  const localCounts = useMemo(() => {
    if (performers || !showMatch || !items) return null;
    const counts = Object.fromEntries(MATCH_STATUSES.map(([k]) => [k, 0]));
    items.forEach(item => { counts[MATCH_LABEL[item.matchStatus] ? item.matchStatus : 'unmatched'] += 1; });
    return counts;
  }, [items, performers, showMatch]);
  const shownAll = useMemo(() => {
    if (!items || performers) return items;
    const q = query.toLowerCase();
    return items.filter(item => (!q || item.name.toLowerCase().includes(q)) && (!status || (MATCH_LABEL[item.matchStatus] ? item.matchStatus : 'unmatched') === status));
  }, [items, performers, query, status]);
  const shownTotal = performers ? total : (shownAll || []).length;
  const shown = performers ? shownAll : (shownAll || []).slice(page * size, page * size + size);
  const allTotal = performers ? (serverCounts ? Object.values(serverCounts).reduce((a, b) => a + b, 0) : null) : (items || []).length;
  const filtered = !!(query || status);
  const loaded = items !== null;
  const nothingAtAll = loaded && !filtered && items.length === 0;

  function openCreate() { if (performers) { setCreatingPerformer(true); return; } setName(''); setFormError(null); setEditing({}); }
  useEffect(() => { if (addRef) addRef.current = openCreate; });
  function openEdit(item) { setName(item.name); setFormError(null); setEditing(item); }

  async function save() {
    setBusy(true); setFormError(null);
    try {
      if (editing.id) await update(editing.id, { name: name.trim() }); else await create({ name: name.trim() });
      notify.success(editing.id ? `${entityLabel} renamed` : `${entityLabel} added`);
      setEditing(null); changed();
    } catch (err) { setFormError(describeError(`save the ${entityLabel.toLowerCase()}`, err)); }
    finally { setBusy(false); }
  }

  const statusOptions = [{ value: '', label: 'All', count: allTotal }, ...MATCH_STATUSES.map(([key, label]) => ({ value: key, label, count: (performers ? serverCounts : localCounts)?.[key] }))];
  return <>
    {notice}
    <Panel bodyless>
      {showMatch && !nothingAtAll && <SegmentedTabs label="Filter by match status" value={status} onChange={v => setFilters({ status: v })} options={statusOptions} />}
      <TableToolbar search={search} onSearch={setSearch} label={`Search ${noun}`} placeholder={performers ? 'Search name or alias' : `Search ${noun}`}
        end={<>{addInToolbar && <Button size="sm" onClick={openCreate} leadingIcon={<Plus size={14} />}>Add {entityLabel.toLowerCase()}</Button>}{tools.length > 0 && <MoreMenu label="Tools" items={tools} />}</>}>
        {filtered && <button type="button" className="console-link" onClick={() => { setSearch(''); setFilters({ q: '', status: '' }); }}>Reset</button>}
      </TableToolbar>
      {error && !loaded ? <ErrorState title={`Couldn't load ${noun}`} error={error} onRetry={load} />
        : !loaded ? <SkeletonRows rows={6} label={`Loading ${noun}`} />
          : nothingAtAll ? <EmptyState icon={Tags} title={libraryId && libraryScoped ? `No ${noun} in this library` : `No ${noun} yet`} message={libraryId && libraryScoped ? 'Switch the library scope to All libraries to see everything.' : 'They are created automatically as the scanner discovers media, or add one by hand.'} action={<Button size="sm" onClick={openCreate}>Add {entityLabel.toLowerCase()}</Button>} />
            : shown.length === 0 ? <EmptyState compact icon={Tags} title={`No ${noun} match`} message={`${query ? `Nothing matches “${query}”` : ''}${status ? ` with status “${MATCH_LABEL[status]}”` : ''}.`} action={<Button size="sm" variant="outline" onClick={() => { setSearch(''); setFilters({ q: '', status: '' }); }}>Clear filters</Button>} />
              : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
                <TableHeader><TableRow><TableHead>Name</TableHead>{showMatch && <TableHead>Match</TableHead>}<TableHead className="console-num-cell">Titles</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
                <TableBody>{shown.map(item => <TableRow key={item.id}>
                  <TableCell><span className="console-title-cell">{avatar && <RowAvatar kind={avatar} item={item} />}{showMatch ? <a className="console-link console-title-cell__name" href={mediaHref(`${linkPrefix}/${item.id}`)} title="Open in the library">{item.name}</a> : <span className="console-title-cell__name">{item.name}</span>}</span></TableCell>
                  {showMatch && <TableCell><button type="button" className="console-plain-button" aria-label={`${item.matchStatus === 'matched' ? 'Review match' : 'Match'} for ${item.name}`} onClick={() => setSettingsId(item.id)}><Status tone={MATCH_TONE[MATCH_LABEL[item.matchStatus] ? item.matchStatus : 'unmatched']}>{MATCH_LABEL[MATCH_LABEL[item.matchStatus] ? item.matchStatus : 'unmatched']}</Status></button></TableCell>}
                  <TableCell className="console-num-cell">{formatNumber(item.mediaCount ?? 0)}</TableCell>
                  <TableCell><MoreMenu iconOnly label={`Actions for ${item.name}`} items={[
                    (performers || studios) && { label: 'Match, photo and merge…', icon: Settings2, onSelect: () => setSettingsId(item.id) },
                    { label: 'Rename…', icon: Pencil, onSelect: () => openEdit(item) }, 'separator', { label: 'Delete…', icon: Trash2, danger: true, onSelect: () => askDelete(item) },
                  ]} /></TableCell>
                </TableRow>)}</TableBody>
              </Table></div>}
      {loaded && shownTotal > 0 && <TablePagination limit={size} offset={page * size} total={shownTotal} noun={(shownTotal === 1 ? entityLabel : entityPlural).toLowerCase()} onChange={({ limit, offset }) => setFilters({ size: String(limit), page: String(offset / limit) })} />}
    </Panel>

    {performers && <NewPerformerDialog open={creatingPerformer} onOpenChange={setCreatingPerformer} onCreated={id => { changed(); setSettingsId(id); }} />}
    {studios && <StudioSettings studioId={settingsId} open={settingsId != null} onOpenChange={next => { if (!next) setSettingsId(null); }} onChanged={changed} onSwitch={id => setSettingsId(id)} onDeleted={() => { setSettingsId(null); changed(); }} />}
    {performers && <PerformerSettings performerId={settingsId} open={settingsId != null} onOpenChange={next => { if (!next) setSettingsId(null); }} onChanged={changed} onSwitch={id => setSettingsId(id)} />}

    <FormDialog open={!!editing} onOpenChange={next => { if (!next) setEditing(null); }} title={editing?.id ? `Rename ${entityLabel.toLowerCase()}` : `Add ${entityLabel.toLowerCase()}`} size="sm"
      submitLabel={editing?.id ? 'Save' : `Add ${entityLabel.toLowerCase()}`} dirty={name.trim() !== (editing?.name || '')} canSubmit={!!name.trim()} submitting={busy} error={formError} onSubmit={save}>
      <Field label="Name" error={name.trim() ? null : 'Enter a name.'}>{props => <Input {...props} autoFocus value={name} onChange={e => setName(e.target.value)} />}</Field>
    </FormDialog>
  </>;
}

// Possible duplicate people, found on request and shown above the table until closed.
function DuplicatesPanel({ open, onClose, onChanged }) {
  const notify = useNotify();
  const [pairs, setPairs] = useState(null);
  const [loading, setLoading] = useState(false);
  const find = useCallback(async () => {
    setLoading(true); setPairs(null);
    try { setPairs((await getPerformerDuplicates()).items || []); }
    catch (err) { notify.error('look for duplicates', err); onClose(); }
    finally { setLoading(false); }
  }, [notify, onClose]);
  useEffect(() => { if (open) find(); }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  function done({ sourceId, result, keepName, sourceName }) {
    notify.success(`Merged ${sourceName} into ${keepName}`, `Moved ${plural(result.titles, 'title')}, updated ${plural(result.nfoUpdated, 'NFO file')}.${result.nfoFailed?.length ? ` ${result.nfoFailed.length} could not be written.` : ''}`);
    setPairs(list => (list || []).filter(pair => pair.a.id !== sourceId && pair.b.id !== sourceId));
    onChanged();
  }
  async function dismiss(pair) { await dismissPerformerDuplicate(pair.a.id, pair.b.id).catch(() => {}); setPairs(list => (list || []).filter(item => item !== pair)); }
  if (!open) return null;
  return <Panel title={<>Possible duplicates{pairs ? <span className="console-segmented-tabs__count"> {pairs.length}</span> : null}</>} subtitle="Names that differ by spelling, spacing or word order. Merging keeps one record and rewrites the NFO files."
    actions={<><Button variant="outline" size="sm" onClick={find} disabled={loading}>{loading ? 'Looking…' : 'Look again'}</Button><Button variant="ghost" size="icon-sm" aria-label="Close duplicates" onClick={onClose}><X size={14} /></Button></>}>
    <DuplicateList pairs={pairs} onDone={done} onDismiss={dismiss} empty="No likely duplicates found." />
  </Panel>;
}

// Entries with no titles: a count (shown in the tools menu) and a typed-confirmation bulk delete.
function useCleanup({ count: getCount, cleanup, revision, onChanged, describe }) {
  const confirm = useConfirm();
  const notify = useNotify();
  const [count, setCount] = useState(null);
  useEffect(() => { getCount().then(r => setCount(r.count)).catch(() => setCount(null)); }, [revision]); // eslint-disable-line react-hooks/exhaustive-deps
  async function open() {
    const ok = await confirm({ title: describe.title(count), consequence: describe.body, affects: describe.affects(count), reversible: false, confirmLabel: 'Delete them', tone: 'danger', typeToConfirm: 'delete' });
    if (!ok) return;
    try { const r = await cleanup(); notify.success(describe.done(r.deleted)); onChanged(); }
    catch (err) { notify.error(describe.failed, err); }
  }
  return { count, open };
}

function useRevision() {
  const [revision, setRevision] = useState(0);
  return [revision, useCallback(() => setRevision(v => v + 1), [])];
}

function Header({ title, subtitle, actions, active }) {
  return <>
    <PageHeader title={title} subtitle={subtitle} actions={actions} />
    <SectionTabs entries={metadataTabs} activePath={active} label="Metadata sections" />
  </>;
}

export function PerformersPage() {
  const [revision, bump] = useRevision();
  const [dupesOpen, setDupesOpen] = useState(false);
  const addRef = useRef(null);
  const { terms, libraryId, libraries } = useAdminLibrary();
  const people = terms.people.toLowerCase();
  const person = terms.person.toLowerCase();
  const match = useMatchJob({ start: startPerformerSync, status: getPerformerSync, onFinished: bump });
  const cleanup = useCleanup({ count: getEmptyPerformers, cleanup: cleanupPerformers, revision, onChanged: bump, describe: {
    done: n => `Deleted ${plural(n, person, people)}`, failed: `clean up ${people}`, title: n => `Delete ${plural(n ?? 0, person, people)} with no titles?`,
    body: 'Their photos, provider matches and details are removed too. Favorites are kept.', affects: n => `${plural(n ?? 0, person, people)} with no titles, in every library`,
  } });
  const closeDupes = useCallback(() => setDupesOpen(false), []);
  return <PageStack>
    <Header active="/admin/metadata/performers" title={terms.people} subtitle={`Link ${people} to metadata providers for photos and details, merge duplicates and clear out leftovers.`}
      actions={<>{libraries.length > 1 && <LibraryScopeSelect />}<Button variant="outline" onClick={() => addRef.current?.()} leadingIcon={<Plus size={14} />}>Add {person}</Button>
        <Button onClick={() => match.run(false)} disabled={match.busy || match.running} title={`Looks up ${people} that have not been checked yet`} leadingIcon={<Sparkles size={14} />}>{match.running ? 'Matching…' : 'Match with providers'}</Button></>} />
    <MatchRunBar job={match.job} running={match.running} noun={people} />
    <DuplicatesPanel open={dupesOpen} onClose={closeDupes} onChanged={bump} />
    <EntityManager showMatch performers avatar="performer" addRef={addRef} revision={revision} onChanged={bump} libraryId={libraryId}
      entityLabel={terms.person} entityPlural={terms.people} list={getPerformers} create={createPerformer} update={updatePerformer} remove={deletePerformer}
      tools={[
        { label: 'Find possible duplicates', icon: Copy, onSelect: () => setDupesOpen(true) },
        { label: 'Retry unresolved matches', icon: RotateCcw, disabled: match.busy || match.running, onSelect: () => match.run(true) },
        { label: `Clean up ${people} with no titles${cleanup.count ? ` (${cleanup.count})` : ''}…`, icon: Eraser, disabled: !cleanup.count, onSelect: cleanup.open },
      ]} />
  </PageStack>;
}

export function StudiosPage() {
  const [revision, bump] = useRevision();
  const addRef = useRef(null);
  const { terms, libraryId, libraries } = useAdminLibrary();
  const studios = terms.studios.toLowerCase();
  const match = useMatchJob({ start: startStudioSync, status: getStudioSync, onFinished: bump });
  return <PageStack>
    <Header active="/admin/metadata/studios" title={terms.studios} subtitle={`Link ${studios} to metadata providers for logos and details, and keep their names tidy.`}
      actions={<>{libraries.length > 1 && <LibraryScopeSelect />}<Button variant="outline" onClick={() => addRef.current?.()} leadingIcon={<Plus size={14} />}>Add {terms.studio.toLowerCase()}</Button>
        <Button onClick={() => match.run(false)} disabled={match.busy || match.running} leadingIcon={<Sparkles size={14} />}>{match.running ? 'Matching…' : 'Match with providers'}</Button></>} />
    <MatchRunBar job={match.job} running={match.running} noun={studios} />
    <EntityManager showMatch avatar="studio" linkPrefix="/studios" addRef={addRef} revision={revision} onChanged={bump} libraryId={libraryId}
      entityLabel={terms.studio} entityPlural={terms.studios} list={getStudios} create={createStudio} update={updateStudio} remove={deleteStudio} />
  </PageStack>;
}

export function SeriesPage() {
  const addRef = useRef(null);
  const { terms, libraryId, libraries } = useAdminLibrary();
  return <PageStack>
    <Header active="/admin/metadata/series" title={terms.seriesPlural} subtitle={`Group related titles into ordered ${terms.seriesPlural.toLowerCase()}.`}
      actions={<>{libraries.length > 1 && <LibraryScopeSelect />}<Button onClick={() => addRef.current?.()} leadingIcon={<Plus size={14} />}>Add {terms.series.toLowerCase()}</Button></>} />
    <EntityManager addRef={addRef} libraryId={libraryId} entityLabel={terms.series} entityPlural={terms.seriesPlural} list={getSeries} create={createSeries} update={updateSeries} remove={deleteSeries} />
  </PageStack>;
}

export function CategoriesPage() {
  const [revision, bump] = useRevision();
  const [filters, setFilters] = useUrlState({ list: 'categories' });
  const tab = filters.list;
  const [totals, setTotals] = useState({});
  const { terms, libraryId, library, libraries } = useAdminLibrary();
  const plural_ = terms.categories.toLowerCase();
  const cleanup = useCleanup({ count: getEmptyCategories, cleanup: cleanupCategories, revision, onChanged: bump, describe: {
    done: n => `Deleted ${plural(n, 'entry', 'entries')}`, failed: `clean up ${plural_}`, title: n => `Delete ${plural(n ?? 0, 'entry', 'entries')} with no titles?`,
    body: 'Entries with sub-categories are kept.', affects: n => `${plural(n ?? 0, 'entry', 'entries')} with no titles in any library`,
  } });
  return <PageStack>
    <Header active="/admin/metadata/categories" title={libraryId ? `${terms.categories} & tags` : 'Categories, genres & tags'} subtitle={`${terms.categories} group titles for browsing; tags are free-form labels on titles.`}
      actions={libraries.length > 1 ? <LibraryScopeSelect /> : undefined} />
    <SegmentedTabs page label="Categories or tags" value={tab} onChange={v => setFilters({ list: v })} options={[{ value: 'categories', label: terms.categories, count: totals.categories }, { value: 'tags', label: 'Tags', count: totals.tags }]} />
    {tab === 'categories'
      ? <EntityManager key="categories" addInToolbar revision={revision} onChanged={bump} libraryId={libraryId} onTotal={n => setTotals(t => ({ ...t, categories: n }))}
        entityLabel={terms.category} entityPlural={terms.categories} list={getCategories} create={createCategory} update={updateCategory} remove={deleteCategory}
        tools={[{ label: `Clean up ${plural_} with no titles${cleanup.count ? ` (${cleanup.count})` : ''}…`, icon: Eraser, disabled: !cleanup.count, onSelect: cleanup.open }]} />
      : <EntityManager key="tags" addInToolbar libraryScoped={false} onTotal={n => setTotals(t => ({ ...t, tags: n }))}
        notice={library ? <p className="console-muted console-small">Tags are shared by every library, so this list shows all of them, not only {library.name}’s.</p> : null}
        entityLabel="Tag" entityPlural="Tags" list={getTags} create={createTag} update={updateTag} remove={deleteTag} />}
  </PageStack>;
}
