import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuLabel, DropdownMenuTrigger } from '@spatika/react';
import { Captions, CheckCheck, Columns3, ExternalLink, Film, Settings2, Sparkles } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { artworkUrl, confirmAdminVideos, getAdminVideos, getStorageLocations, matchAdminVideos } from '../../api';
import { mediaHref } from '../../appLinks';
import MediaSettings from '../../pages/MediaSettings';
import {
  BulkBar, EmptyState, ErrorState, PageHeader, PageStack, Panel, SavedViews, SegmentedTabs, SkeletonRows, Status, TablePagination, TableToolbar, rowActivation, useConfirm,
} from '../components';
import { formatBytes, formatDuration, formatNumber, formatResolution, plural, timeAgo } from '../lib/format';
import { useColumnVisibility } from '../lib/useFilterPresets';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { useDebouncedValue } from '../lib/useDebouncedValue';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';

const STATUSES = [['', 'All', 'all'], ['review', 'Needs review', 'review'], ['filename', 'Filename only', 'filename'], ['matched', 'Matched', 'matched'], ['rejected', 'Rejected', 'rejected']];
const missingOptions = terms => [['', 'Any metadata'], ['performers', `No ${terms.people.toLowerCase()}`], ['studio', 'No studio'], ['poster', 'No poster']];
const TRACK_FILTERS = [['', 'Any tracks'], ['subtitles', 'Has subtitles'], ['nosubtitles', 'No subtitles'], ['multiaudio', 'Several audio tracks'], ['unread', 'Tracks not read yet']];
const SORTS = [['', 'Newest first'], ['oldest', 'Oldest first'], ['title', 'Title'], ['size', 'Largest'], ['duration', 'Longest']];
const SOURCE_LABEL = { NFO: 'NFO', MANUAL: 'Edited', FOLDER: 'Folder', INFERRED: 'Inferred' };

function MetadataStatus({ video }) {
  if (video.suggestions > 0) return <Status tone="warn">{plural(video.suggestions, 'suggestion')}</Status>;
  if (video.matchStatus === 'matched') return <Status tone="ok">Matched</Status>;
  if (video.matchStatus === 'rejected') return <Status tone="crit">Rejected</Status>;
  if (video.metadataSource === 'FILENAME') return <Status tone="neutral">Filename only</Status>;
  return <Status tone="accent">{SOURCE_LABEL[video.metadataSource] || video.metadataSource}</Status>;
}

function Thumb({ video }) {
  const [failed, setFailed] = useState(false);
  if (failed) return <span className="console-thumb console-thumb--empty" aria-hidden="true"><Film size={14} /></span>;
  return <img className="console-thumb" src={artworkUrl(video.id, 'thumbnail', video.posterImageId)} alt="" loading="lazy" onError={() => setFailed(true)} />;
}

function Tracks({ video }) {
  const tracks = video.file?.tracks;
  if (!tracks) return <span className="console-muted" title="Not read yet. Run “Read audio & subtitle tracks” under Jobs.">Not read</span>;
  const notes = [];
  if (tracks.subtitles) notes.push(plural(tracks.subtitles, 'subtitle'));
  if (tracks.audio > 1) notes.push(`${tracks.audio} audio tracks`);
  if (tracks.image) notes.push(`${tracks.image} picture subtitles the browser can’t show`);
  return <span className="console-nowrap" title={notes.join(' · ') || 'No subtitles, one audio track'}>
    {tracks.subtitles > 0 ? <><Captions size={13} aria-hidden="true" /> {tracks.subtitles}</> : <span className="console-muted">No subtitles</span>}{tracks.audio > 1 && <span className="console-muted"> · {tracks.audio} audio</span>}
  </span>;
}

export function VideosPage() {
  const admin = useAdminLibrary();
  const { libraryId, terms } = admin;
  const confirm = useConfirm();
  const notify = useNotify();
  const [filters, setFilters] = useUrlState({ q: '', status: '', missing: '', tracks: '', disk: '', sort: '', page: '0', size: '50' });
  const [search, setSearch] = useState(filters.q);
  const debounced = useDebouncedValue(search.trim(), 300);
  const [selected, setSelected] = useState([]);
  const [editing, setEditing] = useState(null);
  const [busy, setBusy] = useState(false);
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), []);

  useEffect(() => { if (debounced !== filters.q) setFilters({ q: debounced }); }, [debounced]); // eslint-disable-line react-hooks/exhaustive-deps
  const size = Number(filters.size) || 50;
  const page = Number(filters.page) || 0;
  const videos = useResource(() => {
    const query = { limit: size, offset: page * size, q: filters.q, status: filters.status, missing: filters.missing, tracks: filters.tracks, sort: filters.sort, storageLocationId: filters.disk, libraryId: libraryId || '' };
    Object.keys(query).forEach(key => { if (query[key] === '' || query[key] === 0 && key !== 'offset') delete query[key]; });
    return getAdminVideos(query);
  }, [filters.q, filters.status, filters.missing, filters.tracks, filters.sort, filters.disk, page, size, libraryId]);
  useEffect(() => { setSelected([]); }, [filters.q, filters.status, filters.missing, filters.tracks, filters.sort, filters.disk, page, size, libraryId]);

  const COLUMNS = useMemo(() => [
    { id: 'studio', label: terms.studio }, { id: 'people', label: terms.people }, { id: 'length', label: 'Length' }, { id: 'quality', label: 'Quality' },
    { id: 'size', label: 'Size' }, { id: 'tracks', label: 'Tracks' }, { id: 'disk', label: 'Disk', defaultHidden: true }, { id: 'added', label: 'Added', defaultHidden: true }, { id: 'thumb', label: 'Thumbnails' },
  ], [terms]);
  const cols = useColumnVisibility('reelstash-admin-columns:videos', COLUMNS);
  const data = videos.data;
  const items = data?.items || [];
  const counts = data?.counts || {};
  const filtered = !!(filters.q || filters.missing || filters.tracks || filters.disk || libraryId);
  const selectedVideos = items.filter(v => selected.includes(v.id));
  const filenameSelected = selectedVideos.filter(v => v.metadataSource === 'FILENAME');
  const reset = () => { setSearch(''); setFilters({ q: '', missing: '', tracks: '', disk: '', status: '' }); };

  async function findMatches() {
    setBusy(true);
    try { await matchAdminVideos(selected); notify.success(`Looking up ${plural(selected.length, 'title')}`, 'Suggestions appear under Needs review as they come in.'); setSelected([]); }
    catch (err) { notify.error('start matching', err); }
    finally { setBusy(false); }
  }
  async function confirmMetadata() {
    const ok = await confirm({
      title: `Confirm metadata for ${plural(filenameSelected.length, 'title')}?`,
      consequence: 'Their filename-guessed titles are accepted as final and stop appearing in the review queue. Titles that already have metadata are left as they are.',
      affects: `${plural(filenameSelected.length, 'selected title')} with filename-only metadata`, reversible: 'Partly. You can still edit any title afterwards.', confirmLabel: 'Confirm metadata',
    });
    if (!ok) return;
    setBusy(true);
    try { const r = await confirmAdminVideos(selected); notify.success(`Confirmed ${plural(r.confirmed, 'title')}`, r.confirmed < selected.length ? 'Titles that already had metadata were left as they are.' : undefined); setSelected([]); await videos.reload(); }
    catch (err) { notify.error('confirm titles', err); }
    finally { setBusy(false); }
  }

  const colSpan = 3 + COLUMNS.filter(c => cols.isVisible(c.id)).length + 1;
  return <PageStack>
    <PageHeader title="Videos" subtitle={`${libraryId ? `Every title in ${admin.library.name}` : 'Every title across your libraries'}. Open one to edit its metadata, or select several to look up matches.`} />
    <Panel bodyless>
      <SegmentedTabs label="Filter by metadata status" value={filters.status} onChange={status => setFilters({ status })} options={STATUSES.map(([value, label, key]) => ({ value, label, count: data ? counts[key] : undefined }))} />
      <TableToolbar search={search} onSearch={setSearch} label="Search videos" placeholder={`Search title, file, studio or ${terms.person.toLowerCase()}`}
        end={<>
          <SavedViews storageKey="reelstash-admin-views:videos" />
          <DropdownMenu>
            <DropdownMenuTrigger asChild><Button variant="outline" size="sm" leadingIcon={<Columns3 size={14} />}>Columns</Button></DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuLabel>Show columns</DropdownMenuLabel>
              {COLUMNS.map(c => <DropdownMenuCheckboxItem key={c.id} checked={cols.isVisible(c.id)} onSelect={e => e.preventDefault()} onCheckedChange={() => cols.toggle(c.id)}>{c.label}</DropdownMenuCheckboxItem>)}
            </DropdownMenuContent>
          </DropdownMenu>
        </>}>
        <LibraryScopeSelect />
        <select className="console-select" aria-label="Filter by disk" value={filters.disk} onChange={e => setFilters({ disk: e.target.value })}><option value="">All disks</option>{(disks.data || []).map(d => <option key={d.id} value={d.id}>{d.name}</option>)}</select>
        <select className="console-select" aria-label="Filter by missing metadata" value={filters.missing} onChange={e => setFilters({ missing: e.target.value })}>{missingOptions(terms).map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        <select className="console-select" aria-label="Filter by audio and subtitle tracks" value={filters.tracks} onChange={e => setFilters({ tracks: e.target.value })}>{TRACK_FILTERS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        <select className="console-select" aria-label="Sort" value={filters.sort} onChange={e => setFilters({ sort: e.target.value })}>{SORTS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        {(filtered || filters.status) && <button type="button" className="console-link" onClick={reset}>Reset</button>}
        {filters.status === 'review' && counts.review > 0 && <Button size="sm" variant="outline" asChild><Link to="/admin/review">Review all matches</Link></Button>}
      </TableToolbar>
      <BulkBar count={selected.length} noun={selected.length === 1 ? 'title' : 'titles'} onClear={() => setSelected([])}>
        <Button size="sm" disabled={busy} onClick={findMatches} leadingIcon={<Sparkles size={14} />}>Find matches</Button>
        <Button size="sm" variant="outline" disabled={busy || !filenameSelected.length} title={filenameSelected.length ? 'Accept filename-guessed metadata as final' : 'None of the selected titles has filename-only metadata'} onClick={confirmMetadata} leadingIcon={<CheckCheck size={14} />}>Confirm metadata</Button>
      </BulkBar>

      {videos.error && !data ? <ErrorState title="Couldn't load videos" error={videos.error} onRetry={videos.reload} />
        : videos.loading ? <SkeletonRows rows={8} label="Loading videos" />
          : items.length === 0 ? <EmptyState icon={Film} title={filtered || filters.status ? 'No videos match' : 'No videos yet'} message={filtered || filters.status ? 'Try another filter or search.' : 'Add a disk under Storage and scan it to fill the library.'}
            action={(filtered || filters.status) ? <Button size="sm" variant="outline" onClick={reset}>Clear filters</Button> : <Button size="sm" asChild><Link to="/admin/storage">Open storage</Link></Button>} />
            : <div className="console-table-responsive" aria-busy={videos.refreshing || undefined}><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow>
                <TableHead className="console-table-check"><input type="checkbox" aria-label="Select all videos on this page" checked={items.length > 0 && items.every(v => selected.includes(v.id))} onChange={e => setSelected(e.target.checked ? items.map(v => v.id) : [])} /></TableHead>
                <TableHead>Title</TableHead>
                {cols.isVisible('studio') && <TableHead>{terms.studio}</TableHead>}
                {cols.isVisible('people') && <TableHead>{terms.people}</TableHead>}
                {cols.isVisible('length') && <TableHead className="console-num-cell">Length</TableHead>}
                {cols.isVisible('quality') && <TableHead>Quality</TableHead>}
                {cols.isVisible('size') && <TableHead className="console-num-cell">Size</TableHead>}
                {cols.isVisible('tracks') && <TableHead>Tracks</TableHead>}
                {cols.isVisible('disk') && <TableHead>Disk</TableHead>}
                <TableHead>Metadata</TableHead>
                {cols.isVisible('added') && <TableHead>Added</TableHead>}
                <TableHead><span className="console-sr-only">Actions</span></TableHead>
              </TableRow></TableHeader>
              <TableBody>
                {items.map(video => <TableRow key={video.id} {...rowActivation(() => setEditing({ id: video.id, tab: video.suggestions > 0 ? 'match' : 'details' }), { selected: selected.includes(video.id) })} aria-label={`${video.title}, edit metadata`}>
                  <TableCell className="console-table-check"><input type="checkbox" aria-label={`Select ${video.title}`} checked={selected.includes(video.id)} onChange={e => setSelected(s => e.target.checked ? [...s, video.id] : s.filter(x => x !== video.id))} /></TableCell>
                  <TableCell><div className="console-title-cell">{cols.isVisible('thumb') && <Thumb video={video} />}<span className="console-title-cell__copy"><span className="console-title-cell__name" title={video.title}>{video.title}</span><span className="console-title-cell__sub" title={video.file?.relativePath}>{video.file ? video.file.fileName : 'No file'}{video.fileCount > 1 ? ` · ${video.fileCount} files` : ''}</span></span></div></TableCell>
                  {cols.isVisible('studio') && <TableCell className="console-truncate-cell">{video.studioName || '—'}</TableCell>}
                  {cols.isVisible('people') && <TableCell className="console-truncate-cell" title={video.performers.join(', ')}>{video.performers.length ? `${video.performers.slice(0, 2).join(', ')}${video.performers.length > 2 ? ` +${video.performers.length - 2}` : ''}` : '—'}</TableCell>}
                  {cols.isVisible('length') && <TableCell className="console-num-cell">{formatDuration(video.durationSeconds)}</TableCell>}
                  {cols.isVisible('quality') && <TableCell className="console-nowrap">{formatResolution(video.file?.height)}</TableCell>}
                  {cols.isVisible('size') && <TableCell className="console-num-cell">{formatBytes(video.file?.size)}</TableCell>}
                  {cols.isVisible('tracks') && <TableCell><Tracks video={video} /></TableCell>}
                  {cols.isVisible('disk') && <TableCell className="console-truncate-cell">{video.file ? (video.file.available ? video.file.storageLocationName : <Status tone="crit">{video.file.storageLocationName} (missing)</Status>) : '—'}</TableCell>}
                  <TableCell><MetadataStatus video={video} /></TableCell>
                  {cols.isVisible('added') && <TableCell className="console-nowrap console-muted">{timeAgo(video.dateAdded)}</TableCell>}
                  <TableCell><span className="console-nowrap">
                    <Button variant="ghost" size="icon-sm" aria-label={`Edit ${video.title}`} title="Edit metadata" onClick={() => setEditing({ id: video.id, tab: 'details' })}><Settings2 size={15} /></Button>
                    <Button variant="ghost" size="icon-sm" aria-label={`Open ${video.title} in the library`} title="Open in library" onClick={() => window.location.assign(mediaHref(`/library/${video.id}`))}><ExternalLink size={15} /></Button>
                  </span></TableCell>
                </TableRow>)}
              </TableBody>
            </Table></div>}
      {data && <TablePagination limit={size} offset={page * size} total={data.total} noun={data.total === 1 ? 'video' : 'videos'} onChange={({ limit, offset }) => setFilters({ size: String(limit), page: String(offset / limit) }, { keepPage: true })} />}
    </Panel>
    {editing && <MediaSettings mediaItemId={editing.id} initialTab={editing.tab} open canDelete onOpenChange={next => { if (!next) setEditing(null); }} onChanged={videos.reload} onDeleted={() => { setEditing(null); videos.reload(); }} />}
  </PageStack>;
}
