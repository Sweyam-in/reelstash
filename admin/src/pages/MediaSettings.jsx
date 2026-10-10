import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Dialog, DialogBody, DialogContent, DialogHeader, DialogTitle,
  DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger,
  FormField, Input, SegmentedControl, Tabs, TabsContent, TabsList, TabsTrigger, useToast,
} from '@spatika/react';
import { ArrowDown, ArrowUp, Film, ImageUp, ChevronDown, Minus, Plus, RefreshCw, Search, Sparkles, Trash2, Unlink, X } from 'lucide-react';
import {
  artworkUrl, deleteMediaItem, deleteMediaPoster, deleteMediaBackdrop, mediaFrameUrl, requestPreview, setPosterFromFrame, setBackdropFromFrame, uploadMediaPoster, uploadMediaBackdrop, getCategories, getMediaItem, getMediaMatchState, getMediaLinkedRecords, getProviders, linkMediaProvider, getPerformers, getSeries, getStudios, performerImageUrl, setMediaReel, setMediaSeries, studioImageUrl, matchMediaProvider, searchMediaProviders, unmatchMediaProvider, updateMediaMetadata,
} from '../api';
import { SearchCombobox, Thumb } from '../components/SearchCombobox';
import { VideosTab as SeriesVideos } from './SeriesSettings';
import MediaTracksTab from './MediaTracksTab';
import { useDebounce } from './hooks/useDebounce';
import { BLACK_LUMA, formatClock, meanLuma } from '../playerUtils';

function PersonThumb({ person }) {
  return <Thumb name={person.name} round src={person.id && person.profileImageId ? performerImageUrl(person.id, person.profileImageId) : null} />;
}

function nfoNote(result) {
  if (result.nfoFailed) return ` ${result.nfoFailed} NFO ${result.nfoFailed === 1 ? 'file' : 'files'} could not be written.`;
  return result.nfoUpdated ? ` Updated ${result.nfoUpdated} NFO ${result.nfoUpdated === 1 ? 'file' : 'files'}.` : '';
}

// Dropdown of checkable options; nothing checked means "all".
function MultiSelect({ label, allLabel, options, selected, onToggle, disabled }) {
  const summary = !selected.length ? allLabel : selected.length === 1 ? options.find(o => o.id === selected[0])?.name || selected[0] : `${label} · ${selected.length}`;
  return <DropdownMenu>
    <DropdownMenuTrigger asChild><button type="button" className="rs-match-select" disabled={disabled} aria-label={label}>{summary} <ChevronDown size={13} /></button></DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuCheckboxItem checked={!selected.length} onSelect={e => { e.preventDefault(); onToggle(null); }}>{allLabel}</DropdownMenuCheckboxItem>
      {options.map(o => <DropdownMenuCheckboxItem key={o.id} checked={selected.includes(o.id)} onSelect={e => { e.preventDefault(); onToggle(o.id); }}>{o.name}</DropdownMenuCheckboxItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}

function MatchTab({ item, onMatched }) {
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [results, setResults] = useState(null);
  const [busy, setBusy] = useState(false);
  const searchSequence = useRef(0);
  const [state, setState] = useState(null);
  const [records, setRecords] = useState({}); // "provider:externalId" -> the provider's own record
  const [allProviders, setAllProviders] = useState([]);
  const [scope, setScope] = useState([]); // selected provider ids, empty = all
  const [searchType, setSearchType] = useState([]); // selected result kinds, empty = all
  const [removing, setRemoving] = useState(null); // provider id being unlinked, '' = every link
  const [markWrong, setMarkWrong] = useState(true);
  const [merge, setMerge] = useState(true); // when already linked, fill gaps instead of replacing

  const providerName = id => allProviders.find(p => p.id === id)?.name || id;
  const links = state?.links || [];
  const loadState = useCallback(() => getMediaMatchState(item.id).then(setState).catch(() => {}), [item.id]);
  useEffect(() => { setRemoving(null); loadState(); }, [loadState]);
  const linkKey = links.map(l => `${l.provider}:${l.externalId}`).join('|');
  useEffect(() => {
    if (!linkKey) { setRecords({}); return undefined; }
    let cancelled = false;
    getMediaLinkedRecords(item.id).then(r => { if (!cancelled) setRecords(Object.fromEntries((r.items || []).map(rec => [`${rec.provider}:${rec.externalId}`, rec]))); }).catch(() => {});
    return () => { cancelled = true; };
  }, [item.id, linkKey]);
  useEffect(() => { getProviders().then(r => setAllProviders(r.items || [])).catch(() => setAllProviders([])); }, []);
  const providers = allProviders.filter(p => p.enabled && p.kinds.includes('media'));

  async function unmatch(provider) {
    setBusy(true);
    try {
      const result = await unmatchMediaProvider(item.id, { markWrong, provider });
      const kept = result.linksRemaining > 0;
      toast({
        title: provider ? `Unlinked ${providerName(provider)}` : 'Match removed',
        description: kept
          ? `The title is still linked to ${result.linksRemaining} other ${result.linksRemaining === 1 ? 'provider' : 'providers'}, so its details were kept.${markWrong ? ' That record won’t be matched automatically again.' : ''}`
          : `The title is back to its file name${result.namesRestored ? ' and the original file name was restored' : ''}${result.nfoSetAside ? ' and its NFO was set aside as .nfo.wrong-match' : ''}.${markWrong ? ' It won’t be matched automatically again.' : ''}`,
        tone: 'success',
      });
      setRemoving(null);
      await loadState();
      onMatched();
    } catch (err) {
      toast({ title: 'Could not remove match', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function clearFlag() {
    setBusy(true);
    try {
      await unmatchMediaProvider(item.id, { markWrong: false });
      setRemoving(null);
      await loadState();
      onMatched();
      toast({ title: 'Wrong-match flag cleared', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not clear flag', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function search(q, searchScope = scope, types = searchType, quiet = false) {
    const sequence = ++searchSequence.current;
    setBusy(true);
    try {
      // The API takes one result kind per call, so several kinds are searched separately and merged.
      const runs = await Promise.all((types.length ? types : ['all']).map(type => searchMediaProviders(item.id, q, searchScope, type)));
      if (sequence !== searchSequence.current) return;
      const seen = new Set();
      setResults(runs.flatMap(run => run.items || []).filter(c => { const key = `${c.provider}:${c.externalId}`; return seen.has(key) ? false : seen.add(key); }));
      if (!q) setQuery(runs[0]?.query || '');
    } catch (err) {
      if (sequence !== searchSequence.current) return;
      toast(quiet
        ? { id: `media-autosearch-${item.id}`, title: 'Couldn’t search automatically', description: `${String(err.message || 'Search failed').replace(/\.?$/, '.')} Edit the query and press Search to retry.`, tone: 'danger' }
        : { title: 'Search failed', description: err.message, tone: 'danger' });
      setResults(null);
    } finally {
      if (sequence === searchSequence.current) setBusy(false);
    }
  }
  useEffect(() => { setQuery(''); setResults(null); search('', [], [], true); setScope([]); setSearchType([]); }, [item.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const toggle = (list, id) => list.includes(id) ? list.filter(x => x !== id) : [...list, id];

  function chooseScope(id) {
    const next = id === null ? [] : toggle(scope, id);
    // Drop kinds that none of the chosen providers can return.
    const only = next.length === 1 ? next[0] : null;
    const nextType = searchType.filter(t => !(only && only !== 'tmdb' && t === 'shows') && !(only === 'tmdb' && (t === 'scenes' || t === 'jav')));
    setScope(next);
    setSearchType(nextType);
    search(query.trim(), next, nextType);
  }

  function chooseSearchType(id) {
    const next = id === null ? [] : toggle(searchType, id);
    setSearchType(next);
    search(query.trim(), scope, next);
  }

  const alreadyLinked = links.length > 0;

  const [pending, setPending] = useState(null); // candidate awaiting confirmation
  const keeping = alreadyLinked && merge;

  async function apply() {
    const candidate = pending;
    setPending(null);
    setBusy(true);
    try {
      await matchMediaProvider(item.id, candidate.provider, candidate.externalId, keeping);
      toast({ title: `Matched “${candidate.title}”`, description: 'The file, NFO and library entry are updated.', tone: 'success' });
      setResults(null);
      await loadState();
      onMatched();
    } catch (err) {
      toast({ title: 'Could not apply match', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  const matched = state?.status === 'matched';
  const isLast = removing !== null && (removing === '' || links.length <= 1);
  const scopeLabel = scope.length ? scope.map(providerName).join(', ') : 'all providers';
  const inScope = id => (scope.length ? scope.includes(id) : true) && providers.some(p => p.id === id);
  const pluginInScope = providers.some(p => p.id !== 'tmdb' && inScope(p.id));
  const tmdbInScope = inScope('tmdb');
  const searchTypes = [
    { id: 'movies', name: 'Movies' },
    ...(pluginInScope ? [{ id: 'scenes', name: 'Scenes' }, { id: 'jav', name: 'JAV' }] : []),
    ...(tmdbInScope ? [{ id: 'shows', name: 'TV Shows' }] : []),
  ];

  async function linkOnly(candidate) {
    setBusy(true);
    try {
      await linkMediaProvider(item.id, candidate.provider, candidate.externalId);
      toast({ title: `Linked to ${providerName(candidate.provider)}`, description: 'The file and the title’s details were not changed.', tone: 'success' });
      await loadState();
    } catch (err) {
      toast({ title: 'Could not link', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  const renderCandidate = candidate => <div key={`${candidate.provider}:${candidate.externalId}`} className="rs-performer-candidate">
        {candidate.poster ? <img src={candidate.poster} alt="" loading="lazy" /> : <span className="rs-directory-mark">{candidate.title.slice(0, 2).toUpperCase()}</span>}
        <span>
          <strong>{candidate.title}</strong>
          <small>{[providers.length > 1 && providerName(candidate.provider), candidate.kind === 'scene' ? 'Scene' : candidate.kind === 'movie' ? 'Movie' : candidate.kind === 'show' ? 'TV Show' : candidate.kind === 'jav' ? 'JAV' : null, candidate.site, candidate.date].filter(Boolean).join(' · ')}</small>
          {candidate.rejected && <small>You marked this as a wrong match before</small>}
          {links.some(l => l.provider === candidate.provider && l.externalId === candidate.externalId) && <small>Currently linked</small>}
          {!!candidate.performers?.length && <small>{candidate.performers.slice(0, 5).join(', ')}</small>}
        </span>
        <span className="rs-match-actions">
          <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => setPending(candidate)}>{alreadyLinked && merge ? 'Add details' : 'Use this match'}</button>
          {matched && !links.some(l => l.provider === candidate.provider) && <button type="button" className="rs-button rs-button-outline" disabled={busy} title="Link this record without changing the file or the title’s details" onClick={() => linkOnly(candidate)}>Link only</button>}
        </span>
      </div>;

  return <div className="rs-pm-merge">
    <form className="rs-match-search" onSubmit={event => { event.preventDefault(); search(query.trim()); }}>
      <div className="rs-match-bar">
        <Search size={15} className="rs-match-bar-icon" />
        <input aria-label={`Search ${scopeLabel}`} value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${scopeLabel}`} />
        {providers.length > 1 && <MultiSelect label="Providers" allLabel="All providers" options={providers} selected={scope} disabled={busy} onToggle={chooseScope} />}
        <MultiSelect label="Type" allLabel="All types" options={searchTypes} selected={searchType} disabled={busy} onToggle={chooseSearchType} />
      </div>
      <button className="rs-button rs-button-primary" type="submit" disabled={busy}>{busy ? 'Searching…' : 'Search'}</button>
    </form>
    <p className="rs-pm-muted">Applying a match renames the file, rewrites its NFO, and replaces the title, studio, performers and categories.</p>
    {!matched && !!state?.suggestions?.length && <section className="rs-mm-suggested">
      <p className="rs-eyebrow"><Sparkles size={13} /> Suggested by the last match job</p>
      <p className="rs-pm-muted">Nothing was applied. Pick the right record, or search below.</p>
      <div className="rs-performer-candidates">{state.suggestions.map(renderCandidate)}</div>
    </section>}
    {results && !results.length && <p className="rs-pm-muted">No results for that search.</p>}
    {!!results?.length && <div className="rs-performer-candidates">
      {results.map(renderCandidate)}
    </div>}
    {state?.status === 'rejected' && <p className="rs-pm-muted">Marked as a wrong match. Automatic matching skips this title.</p>}
    {matched && removing === null && (links.length
      ? <ul className="rs-match-links">{links.map(link => {
        const rec = records[`${link.provider}:${link.externalId}`];
        return <li key={link.provider} className={rec?.title ? 'rs-match-linked' : undefined}>
          {rec?.title && (rec.poster ? <img src={rec.poster} alt="" loading="lazy" /> : <span className="rs-directory-mark">{rec.title.slice(0, 2).toUpperCase()}</span>)}
          <span>
            <span>Linked to <strong>{providerName(link.provider)}</strong></span>
            {rec?.title && <strong>{rec.url ? <a href={rec.url} target="_blank" rel="noreferrer">{rec.title}</a> : rec.title}</strong>}
            {rec?.title && <small>{[rec.site, rec.date].filter(Boolean).join(' · ')}</small>}
            {!!rec?.performers?.length && <small>{rec.performers.slice(0, 5).join(', ')}</small>}
            {rec?.error && <small>Couldn’t load the record: {rec.error}</small>}
          </span>
          <button type="button" className="rs-button rs-button-danger" onClick={() => { setMarkWrong(true); setRemoving(link.provider); }}><Unlink size={15} /> Unlink {providerName(link.provider)}</button>
        </li>;
      })}</ul>
      : <ul className="rs-match-links"><li>
          <span><>Matched from its <strong>NFO file</strong>, not linked to any provider. Search below and choose <strong>Link only</strong> to link it without changing anything.</></span>
          <button type="button" className="rs-button rs-button-danger" onClick={() => { setMarkWrong(true); setRemoving(''); }}><Unlink size={15} /> Remove match</button>
        </li></ul>)}
    {state?.status === 'rejected' && removing === null && <button type="button" className="rs-button rs-button-danger" onClick={() => setRemoving('')}><Unlink size={15} /> Clear the wrong-match flag</button>}
    {removing !== null && <div className="rs-pm-confirm">
      <p>{!matched
        ? 'The title stays as it is. Automatic matching may pick it up again.'
        : isLast
          ? 'This clears the matched title, studio, description, date, performers, categories and poster, and puts the title back to its file name. The file keeps its current name, and its NFO is set aside (not deleted).'
          : `Only the ${providerName(removing)} link is removed. The title keeps its current details because it is still linked to another provider.`}</p>
      {matched && <label className="rs-pm-check"><input type="checkbox" checked={markWrong} onChange={e => setMarkWrong(e.target.checked)} /> Mark as a wrong match, so automatic jobs never match this record again</label>}
      <div>
        <button type="button" className="rs-button rs-button-primary" disabled={busy} onClick={matched ? () => unmatch(removing) : clearFlag}>{busy ? 'Removing…' : matched ? (removing ? `Unlink ${providerName(removing)}` : 'Remove match') : 'Clear flag'}</button>
        <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => setRemoving(null)}>Cancel</button>
      </div>
    </div>}
    {alreadyLinked && <label className="rs-pm-check"><input type="checkbox" checked={merge} onChange={e => setMerge(e.target.checked)} /> Keep this title’s current details and only fill in what is missing</label>}
    <AlertDialog open={!!pending} onOpenChange={next => { if (!next) setPending(null); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{keeping ? 'Add details from' : 'Apply'} “{pending?.title}”?</AlertDialogTitle>
          <AlertDialogDescription>{keeping
            ? `From ${providerName(pending?.provider)}. This keeps the title’s current details, fills in what is missing, combines performers and categories, and rewrites its NFO.`
            : `From ${providerName(pending?.provider)}. This renames the file, rewrites its NFO, and replaces this title’s performers and categories.`}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={apply}>{keeping ? 'Add details' : 'Apply match'}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>;
}

// True when the image at src is (almost) all black, so the person is told before they have to squint at it.
function useLooksBlack(src) {
  const [black, setBlack] = useState(false);
  useEffect(() => {
    setBlack(false);
    let cancelled = false;
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = 32;
        canvas.height = 18;
        const context = canvas.getContext('2d', { willReadFrequently: true });
        context.drawImage(img, 0, 0, 32, 18);
        if (!cancelled) setBlack(meanLuma(context.getImageData(0, 0, 32, 18).data) < BLACK_LUMA);
      } catch { /* unreadable image: say nothing */ }
    };
    img.src = src;
    return () => { cancelled = true; };
  }, [src]);
  return black;
}

// Preview any moment of the video and use it for either artwork slot.
function FramePicker({ item, busy, run, kind = 'poster' }) {
  const file = item.files?.find(f => f.available);
  const duration = Number(item.durationSeconds || file?.durationSeconds || 0);
  const [seconds, setSeconds] = useState(() => Math.round(duration * 0.1 * 2) / 2);
  const shown = useDebounce(seconds, 250);
  const [frameState, setFrameState] = useState('loading'); // loading | ready | error
  const src = mediaFrameUrl(item.id, shown);
  useEffect(() => { setFrameState('loading'); }, [src]);
  const max = duration > 0 ? duration : 600;
  const nudge = delta => setSeconds(value => Math.min(max, Math.max(0, Math.round((value + delta) * 10) / 10)));
  const setFrame = kind === 'backdrop' ? setBackdropFromFrame : setPosterFromFrame;
  const label = kind === 'backdrop' ? 'Backdrop' : 'Poster';

  if (!file) return <p className="rs-pm-muted">The video file isn’t available right now, so a frame can’t be picked from it.</p>;
  return <div className="rs-pm-frame">
    <div className={`rs-pm-frame-preview is-${frameState}`}>
      <img src={src} alt={`Frame at ${formatClock(shown)}`} onLoad={() => setFrameState('ready')} onError={() => setFrameState('error')} />
      {frameState === 'loading' && <span>Reading frame…</span>}
      {frameState === 'error' && <span>Couldn’t read a frame there.</span>}
    </div>
    <input className="rs-pm-frame-slider" type="range" min={0} max={max} step={0.5} value={Math.min(seconds, max)} aria-label={`${label} position in the video`} onChange={event => setSeconds(Number(event.target.value))} />
    <div className="rs-pm-frame-row">
      <span className="rs-pm-frame-time">{formatClock(seconds)}{duration > 0 && <small> / {formatClock(duration)}</small>}</span>
      <span className="rs-pm-actions">
        <button type="button" className="rs-pm-icon" aria-label={`${label}: back 1 second`} disabled={busy || seconds <= 0} onClick={() => nudge(-1)}><Minus size={15} /></button>
        <button type="button" className="rs-pm-icon" aria-label={`${label}: forward 1 second`} disabled={busy || seconds >= max} onClick={() => nudge(1)}><Plus size={15} /></button>
        <Button disabled={busy || frameState === 'error'} onClick={() => run(() => setFrame(item.id, { seconds }), `${label} set from ${formatClock(seconds)}`, label)}><Film size={15} /> Use as {kind}</Button>
        <Button variant="secondary" disabled={busy} onClick={() => run(async () => { const result = await setFrame(item.id, { auto: true }); setSeconds(Math.round(result.seconds * 2) / 2); }, `${label} set from a clear frame`, label)}><Sparkles size={15} /> Auto-pick {kind} frame</Button>
      </span>
    </div>
  </div>;
}

function PosterTab({ item, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const posterSrc = artworkUrl(item.id, 'poster', item.posterImageId);
  const looksBlack = useLooksBlack(posterSrc);

  async function run(action, success, label = 'Poster') {
    setBusy(true);
    try {
      await action();
      onChanged();
      toast({ title: success, tone: 'success' });
    } catch (err) {
      toast({ title: `Could not update ${label.toLowerCase()}`, description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadMediaPoster(item.id, file), 'Poster saved');
  }

  function uploadBackdrop(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadMediaBackdrop(item.id, file), 'Backdrop saved', 'Backdrop');
  }

  return <div className="rs-pm-poster-tab">
    <section><p className="rs-eyebrow">Poster</p>
    <div className="rs-pm-photo">
      <div className="rs-pm-photo-frame"><img className="rs-pm-poster" src={posterSrc} alt="" /></div>
      <div>
        {looksBlack && <p className="rs-pm-warning" role="status">This poster looks black. Pick another moment from the video below, or let ReelStash choose one.</p>}
        <p className="rs-pm-muted">Set your own poster from the video or an image. It replaces the current one and is never replaced by a metadata provider match or a rescan. Images can be JPEG, PNG, WebP or GIF, up to 8 MB.</p>
        <div className="rs-pm-actions">
          <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
            <ImageUp size={16} /> {busy ? 'Working…' : 'Choose image'}
            <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={upload} />
          </label>
          {item.posterImageId && <Button variant="secondary" disabled={busy} onClick={() => run(() => deleteMediaPoster(item.id), 'Poster removed')}><Trash2 size={15} /> Remove poster</Button>}
          <Button variant="secondary" disabled={busy || !item.files?.some(f => f.available)} onClick={() => run(() => requestPreview(item.id), 'Hover preview is being built')}><Film size={15} /> Build hover preview</Button>
        </div>
      </div>
    </div>
    <section>
      <p className="rs-eyebrow">From the video</p>
      <FramePicker key={item.id} item={item} busy={busy} run={run} />
    </section>
    </section>
    <section className="rs-pm-backdrop-section">
      <p className="rs-eyebrow">Backdrop</p>
      <div className="rs-pm-photo">
        <div className="rs-pm-photo-frame rs-pm-backdrop-frame">
          {item.backdropImageId ? <img src={artworkUrl(item.id, 'backdrop', item.backdropImageId)} alt="Current backdrop" /> : <span className="rs-pm-muted">No backdrop set</span>}
        </div>
        <div>
          <p className="rs-pm-muted">The backdrop fills the video page behind its title. Choose a wide image or take a frame from the video. A backdrop you set is kept when metadata is matched again. Images can be JPEG, PNG, WebP or GIF, up to 8 MB.</p>
          <div className="rs-pm-actions">
            <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
              <ImageUp size={16} /> {busy ? 'Working…' : 'Choose backdrop image'}
              <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={uploadBackdrop} />
            </label>
            {item.backdropImageId && <Button variant="secondary" disabled={busy} onClick={() => run(() => deleteMediaBackdrop(item.id), 'Backdrop removed', 'Backdrop')}><Trash2 size={15} /> Remove backdrop</Button>}
          </div>
        </div>
      </div>
      <p className="rs-eyebrow">From the video</p>
      <FramePicker key={`${item.id}-backdrop`} item={item} busy={busy} run={run} kind="backdrop" />
    </section>
  </div>;
}

function formatBytes(bytes) {
  if (!bytes && bytes !== 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit += 1; }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatBitrate(bps) {
  if (!bps && bps !== 0) return '—';
  return bps >= 1_000_000 ? `${(bps / 1_000_000).toFixed(1)} Mbps` : `${(bps / 1000).toFixed(0)} kbps`;
}

function FileTab({ item, canDelete, onDeleted }) {
  const files = item.files || [];
  return <div className="rs-pm-merge rs-pm-files">
    {!files.length && <p className="rs-pm-muted">No files on record for this title.</p>}
    {files.map(file => <div key={file.id} className="rs-pm-file">
      {!file.available && <p className="rs-pm-warning" role="status">This file is missing from disk.</p>}
      <dl className="rs-pm-file-grid">
        <dt>Path</dt><dd>{file.storageLocation} / {file.relativePath}</dd>
        <dt>File name</dt><dd>{file.fileName}</dd>
        {file.originalFileName && file.originalFileName !== file.fileName && <><dt>Original name</dt><dd>{file.originalFileName}</dd></>}
        <dt>Format</dt><dd>{(file.container || file.extension || '—').toUpperCase()}</dd>
        <dt>Size</dt><dd>{formatBytes(file.fileSize)}</dd>
        <dt>Resolution</dt><dd>{file.width && file.height ? `${file.width}×${file.height}` : '—'}</dd>
        <dt>Duration</dt><dd>{file.durationSeconds ? formatClock(file.durationSeconds) : '—'}</dd>
        <dt>Frame rate</dt><dd>{file.frameRate ? `${file.frameRate.toFixed(2)} fps` : '—'}</dd>
        <dt>Video codec</dt><dd>{file.videoCodec || '—'}</dd>
        <dt>Audio codec</dt><dd>{file.audioCodec || '—'}</dd>
        <dt>Video bitrate</dt><dd>{formatBitrate(file.videoBitrate)}</dd>
        <dt>Audio bitrate</dt><dd>{formatBitrate(file.audioBitrate)}</dd>
        <dt>Overall bitrate</dt><dd>{formatBitrate(file.overallBitrate)}</dd>
        <dt>Modified</dt><dd>{file.modifiedTime ? new Date(file.modifiedTime).toLocaleString() : '—'}</dd>
      </dl>
    </div>)}
    {canDelete && <DeleteTab item={item} onDeleted={onDeleted} />}
  </div>;
}

function DeleteTab({ item, onDeleted }) {
  const { toast } = useToast();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  async function remove() {
    setBusy(true);
    try {
      await deleteMediaItem(item.id);
      toast({ title: 'Video deleted', description: `“${item.title}” was removed from disk and the library.`, tone: 'success' });
      onDeleted();
    } catch (err) {
      toast({ title: 'Could not delete video', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  return <div className="rs-pm-merge">
    <p className="rs-pm-muted">Permanently deletes this title’s video file{item.files?.length > 1 ? 's' : ''} and NFO from disk and removes it from the library. This can’t be undone.</p>
    {!confirming
      ? <button type="button" className="rs-button rs-button-danger" onClick={() => setConfirming(true)}><Trash2 size={15} /> Delete video</button>
      : <div className="rs-pm-confirm">
        <p>Delete “{item.title}” for good? Its files are removed from disk.</p>
        <div>
          <button type="button" className="rs-button rs-button-danger" disabled={busy} onClick={remove}>{busy ? 'Deleting…' : 'Yes, delete permanently'}</button>
          <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => setConfirming(false)}>Cancel</button>
        </div>
      </div>}
  </div>;
}

function ChipEditor({ label, values, onChange, suggestions, placeholder }) {
  const [text, setText] = useState('');
  function add() {
    const value = text.trim();
    if (value && !values.some(existing => existing.toLowerCase() === value.toLowerCase())) onChange([...values, value]);
    setText('');
  }
  const listId = `rs-chip-${label.replace(/\W/g, '')}`;
  return <div>
    <p className="rs-eyebrow">{label}</p>
    <p className="rs-pm-chips rs-pm-chips-edit">
      {values.map(value => <span key={value}>{value}<button type="button" aria-label={`Remove ${value}`} onClick={() => onChange(values.filter(v => v !== value))}><X size={12} /></button></span>)}
      {!values.length && <small className="rs-pm-muted">None</small>}
    </p>
    <div className="rs-match-search">
      <input list={listId} value={text} placeholder={placeholder} onChange={e => setText(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add(); } }} />
      <datalist id={listId}>{suggestions.map(name => <option key={name} value={name} />)}</datalist>
      <button type="button" className="rs-button rs-button-outline" disabled={!text.trim()} onClick={add}><Plus size={15} /> Add</button>
    </div>
  </div>;
}

function DetailsTab({ draft, update, onSubmit, files = [] }) {
  const originals = files.filter(f => f.originalFileName && f.originalFileName !== f.fileName);
  const [categoryNames, setCategoryNames] = useState([]);

  useEffect(() => {
    getCategories().then(r => setCategoryNames((r.items || []).map(c => c.name))).catch(() => {});
  }, []);

  return <form className="rs-pm-form" onSubmit={event => { event.preventDefault(); onSubmit(); }}>
    <FormField label="Title"><Input value={draft.title} onChange={e => update({ title: e.target.value })} /></FormField>
    <FormField label="Description"><textarea className="rs-pm-textarea" rows={5} value={draft.description} onChange={e => update({ description: e.target.value })} placeholder="Optional" /></FormField>
    <ChipEditor label="Categories" values={draft.categories} onChange={categories => update({ categories })} suggestions={categoryNames} placeholder="Add a category" />
    {!!originals.length && <div className="rs-pm-original">
      <p className="rs-eyebrow">Original file name{originals.length > 1 ? 's' : ''}</p>
      {originals.map(file => <p key={file.id} className="rs-pm-original-name">{file.originalFileName}</p>)}
      <p className="rs-pm-muted">What the file was called before a metadata match renamed it. Removing the match restores it.</p>
    </div>}
  </form>;
}

function StudioTab({ draft, update }) {
  const { toast } = useToast();
  const selected = draft.studio;

  const searchStudios = useCallback(async q => {
    try { return (await getStudios({ q, limit: 8 })).items || []; }
    catch (err) { toast({ title: 'Studio search failed', description: err.message, tone: 'danger' }); return []; }
  }, [toast]);

  function choose(studio) {
    update({ studio: { name: studio.name, id: studio.id || 0, logoImageId: studio.logoImageId || null } });
  }

  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">{selected.name ? 'Change studio' : 'Set a studio'}</p>
      <SearchCombobox
        placeholder="Search studios"
        searchFn={searchStudios}
        onPick={choose}
        createLabel={typed => `Create & use “${typed}”`}
        renderOption={studio => <><Thumb name={studio.name} src={studio.logoImageId ? studioImageUrl(studio.id, studio.logoImageId) : null} /><span><strong>{studio.name}</strong></span>{selected.name.toLowerCase() === studio.name.toLowerCase() && <small>Selected</small>}</>}
      />
    </section>
    <section>
      <p className="rs-eyebrow">Studio of this title</p>
      {selected.name ? <div className="rs-pm-found"><div className="rs-pm-found-row rs-pm-title-row">
        <span className="rs-pm-who"><Thumb name={selected.name} src={selected.id && selected.logoImageId ? studioImageUrl(selected.id, selected.logoImageId) : null} /><span><strong>{selected.name}</strong>{!selected.id && <small>New studio</small>}</span></span>
        <span className="rs-pm-actions"><button type="button" className="rs-pm-icon" aria-label={`Remove ${selected.name}`} title="Remove studio" onClick={() => update({ studio: { name: '', id: 0, logoImageId: null } })}><X size={15} /></button></span>
      </div></div> : <p className="rs-pm-muted">No studio.</p>}
    </section>
  </div>;
}

function PerformersTab({ draft, update }) {
  const { toast } = useToast();
  const cast = draft.cast;
  const setCast = fn => update(current => ({ cast: fn(current.cast) }));

  const has = candidate => cast.some(p => (candidate.id && p.id === candidate.id) || p.name.toLowerCase() === candidate.name.toLowerCase());

  const searchPerformers = useCallback(async q => {
    try { return (await getPerformers({ q, limit: 8 })).items || []; }
    catch (err) { toast({ title: 'Performer search failed', description: err.message, tone: 'danger' }); return []; }
  }, [toast]);

  function add(performer) {
    if (!has(performer)) setCast(list => [...list, { id: performer.id || 0, name: performer.name, profileImageId: performer.profileImageId }]);
  }

  function move(index, delta) {
    setCast(list => {
      const next = [...list];
      const [moved] = next.splice(index, 1);
      next.splice(index + delta, 0, moved);
      return next;
    });
  }

  return <div className="rs-pm-merge rs-pm-performers">
    <section>
      <p className="rs-eyebrow">Add a performer</p>
      <SearchCombobox
        placeholder="Search performers"
        searchFn={searchPerformers}
        onPick={add}
        createLabel={typed => `Create & add “${typed}”`}
        renderOption={person => <><PersonThumb person={person} /><span><strong>{person.name}</strong></span>{has(person) && <small>Added</small>}</>}
      />
    </section>
    <section>
      <p className="rs-eyebrow">In this title · {cast.length}</p>
      {!cast.length && <p className="rs-pm-muted">No performers yet.</p>}
      <div className="rs-pm-found">
        {cast.map((person, index) => <div key={`${person.id}-${person.name}`} className="rs-pm-found-row rs-pm-title-row">
          <span className="rs-pm-who"><PersonThumb person={person} /><span><strong>{person.name}</strong>{!person.id && <small>New performer</small>}</span></span>
          <span className="rs-pm-actions">
            <button type="button" className="rs-pm-icon" aria-label={`Move ${person.name} up`} disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp size={15} /></button>
            <button type="button" className="rs-pm-icon" aria-label={`Move ${person.name} down`} disabled={index === cast.length - 1} onClick={() => move(index, 1)}><ArrowDown size={15} /></button>
            <button type="button" className="rs-pm-icon" aria-label={`Remove ${person.name}`} title="Remove" onClick={() => setCast(list => list.filter((_, i) => i !== index))}><X size={15} /></button>
          </span>
        </div>)}
      </div>
    </section>
  </div>;
}

/**
 * Puts the title in a series (or a new one) and sets its number there. Unlike the Details, Performers and
 * Studio tabs, series changes save right away, and the list below reorders the whole series.
 */
const REEL_CHOICES = [
  { value: 'auto', label: 'Automatic' },
  { value: 'on', label: 'Always a reel' },
  { value: 'off', label: 'Never a reel' },
];

// Whether the title plays under Reels. Saved right away, like the Series tab.
function ReelTab({ item, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const value = item.reelOverride == null ? 'auto' : item.reelOverride ? 'on' : 'off';
  async function choose(next) {
    if (next === value || busy) return;
    setBusy(true);
    try {
      const updated = await setMediaReel(item.id, next === 'auto' ? null : next === 'on');
      toast({ title: updated.reel ? 'Shows in Reels' : 'Kept out of Reels', tone: 'success' });
      await onChanged();
    } catch (err) {
      toast({ title: 'Could not update Reels', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }
  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">Reels</p>
      <SegmentedControl aria-label="Show in Reels" value={value} onChange={choose} options={REEL_CHOICES.map(choice => ({ ...choice, disabled: busy }))} />
      <p className="rs-pm-muted">{item.reelDetected ? 'Detected as a portrait video.' : 'Not detected as a portrait video.'} {item.reel ? 'It shows under Reels' : 'It does not show under Reels'}, and stays in the library either way. Automatic follows the video’s shape.</p>
    </section>
  </div>;
}

function SeriesTab({ item, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [number, setNumber] = useState(item.seriesPosition ?? '');
  const [listKey, setListKey] = useState(0);
  const savingNumber = useRef(null); // the number being saved, so Enter and the blur that follows save once
  useEffect(() => { setNumber(item.seriesPosition ?? ''); savingNumber.current = null; }, [item.seriesPosition]);

  const searchSeries = useCallback(async q => {
    try { return (await getSeries({ q, limit: 8 })).items || []; }
    catch (err) { toast({ title: 'Series search failed', description: err.message, tone: 'danger' }); return []; }
  }, [toast]);

  async function run(payload, success) {
    setBusy(true);
    try {
      await setMediaSeries(item.id, payload);
      toast({ title: success, tone: 'success' });
      await onChanged();
      setListKey(k => k + 1);
    } catch (err) {
      toast({ title: 'Could not update the series', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function saveNumber(event) {
    event?.preventDefault();
    const value = String(number).trim();
    if (value === String(item.seriesPosition ?? '') || value === savingNumber.current) return;
    const parsed = Number.parseInt(value, 10);
    if (value !== '' && (!Number.isFinite(parsed) || parsed < 0)) { setNumber(item.seriesPosition ?? ''); return; }
    savingNumber.current = value;
    run({ seriesId: item.seriesId, position: value === '' ? 0 : parsed }, value === '' ? 'Number cleared' : `Now number ${parsed}`)
      .finally(() => { savingNumber.current = null; });
  }

  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">{item.seriesId ? 'Move to another series' : 'Add to a series'}</p>
      <SearchCombobox
        placeholder="Search series"
        searchFn={searchSeries}
        onPick={series => run(series.id ? { seriesId: series.id } : { seriesName: series.name }, series.id ? `Added to ${series.name}` : `Created ${series.name}`)}
        createLabel={typed => `Create series “${typed}”`}
        renderOption={series => <><Thumb name={series.name} /><span><strong>{series.name}</strong><small>{series.mediaCount} {series.mediaCount === 1 ? 'video' : 'videos'}</small></span>{series.id === item.seriesId && <small>Current</small>}</>}
      />
    </section>
    <section>
      <p className="rs-eyebrow">Series of this video</p>
      {item.seriesId ? <div className="rs-pm-found"><div className="rs-pm-found-row rs-pm-title-row">
        <span className="rs-pm-who"><Thumb name={item.seriesName} /><span><Link to={`/series/${item.seriesId}`}><strong>{item.seriesName}</strong></Link><small>Match the whole series with a provider and pair its scenes or episodes on the series page.</small></span></span>
        <form className="rs-series-number-form" onSubmit={saveNumber}>
          <label>No. <input type="number" min="1" inputMode="numeric" aria-label="Number in the series" value={number} disabled={busy} onChange={e => setNumber(e.target.value)} onBlur={saveNumber} /></label>
        </form>
        <span className="rs-pm-actions"><button type="button" className="rs-pm-icon" aria-label={`Remove from ${item.seriesName}`} title="Remove from series" disabled={busy} onClick={() => run({ seriesId: 0 }, `Removed from ${item.seriesName}`)}><X size={15} /></button></span>
      </div></div> : <p className="rs-pm-muted">Not in a series. Pick one above or type a name to create one, for example when this video is one scene of a movie.</p>}
    </section>
    {item.seriesId && <SeriesVideos key={`${item.seriesId}-${listKey}`} series={{ id: item.seriesId }} current={item.id} showAdd={false} onChanged={onChanged} />}
  </div>;
}

// The editable fields staged across the Details, Performers and Studio tabs; one Save writes them all.
function draftFrom(item) {
  return {
    title: item.title || '',
    description: item.description || '',
    categories: item.categories || [],
    cast: (item.cast || []).map(p => ({ id: p.id, name: p.name, profileImageId: p.profileImageId })),
    studio: { name: item.studioName || '', id: item.studioId || 0, logoImageId: item.studioLogoImageId || null },
  };
}

const sameField = {
  title: (a, b) => a === b,
  description: (a, b) => a === b,
  categories: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  cast: (a, b) => a.length === b.length && a.every((p, i) => p.id === b[i].id && p.name === b[i].name),
  studio: (a, b) => a.name === b.name,
};

function dirtyFields(form) {
  return form ? Object.keys(sameField).filter(key => !sameField[key](form.draft[key], form.base[key])) : [];
}

/**
 * The title settings modal: metadata provider match, details (title, description, categories), studio,
 * performers, and the audio and subtitle tracks. Edits on the Details, Performers and Studio tabs are staged together and written by the
 * header's Save button; every save writes the database and the title's NFO files.
 * `onChanged` fires after an edit so the page behind can refresh.
 */
export default function MediaSettings({ mediaItemId, open, onOpenChange, onChanged, canDelete, onDeleted, initialTab = 'details' }) {
  const { toast } = useToast();
  const [item, setItem] = useState(null);
  // `base` is the saved state the draft was built from, so a reload (after a poster or match change) keeps edits.
  const [form, setForm] = useState(null);
  const [tab, setTab] = useState('details');
  const [saving, setSaving] = useState(false);
  const [confirmClose, setConfirmClose] = useState(false);

  const load = useCallback(({ reset = false } = {}) => getMediaItem(mediaItemId).then(result => {
    setItem(result);
    const fresh = draftFrom(result);
    setForm(prev => {
      if (reset || !prev) return { base: fresh, draft: fresh };
      const draft = { ...fresh };
      for (const key of dirtyFields(prev)) draft[key] = prev.draft[key];
      return { base: fresh, draft };
    });
    return result;
  }), [mediaItemId]);
  useEffect(() => {
    if (!open) return;
    setTab(initialTab);
    setItem(null);
    setForm(null);
    load({ reset: true }).catch(err => {
      toast({ title: 'Could not load title', description: err.message, tone: 'danger' });
      onOpenChange(false);
    });
  }, [open, load]); // eslint-disable-line react-hooks/exhaustive-deps

  const changed = useCallback(() => load().then(() => onChanged?.()).catch(err => toast({ title: 'Could not refresh title', description: err.message, tone: 'danger' })), [load, onChanged, toast]);

  const update = useCallback(patch => setForm(prev => ({
    ...prev,
    draft: { ...prev.draft, ...(typeof patch === 'function' ? patch(prev.draft) : patch) },
  })), []);

  const dirty = dirtyFields(form);
  const canSave = dirty.length > 0 && !saving && !!form?.draft.title.trim();

  async function save() {
    if (!canSave) return;
    const { draft } = form;
    const patch = {};
    if (dirty.includes('title')) patch.title = draft.title;
    if (dirty.includes('description')) patch.description = draft.description;
    if (dirty.includes('categories')) patch.categories = draft.categories;
    if (dirty.includes('cast')) patch.performers = draft.cast;
    if (dirty.includes('studio')) patch.studio = draft.studio.name;
    setSaving(true);
    try {
      const result = await updateMediaMetadata(item.id, patch);
      toast({ title: 'Changes saved', description: nfoNote(result).trim() || undefined, tone: result.nfoFailed ? 'warn' : 'success' });
      await load({ reset: true });
      onChanged?.();
    } catch (err) {
      toast({ title: 'Could not save changes', description: err.message, tone: 'danger' });
    } finally {
      setSaving(false);
    }
  }

  function requestOpenChange(next) {
    if (!next && dirty.length) setConfirmClose(true);
    else onOpenChange(next);
  }

  const edited = key => dirty.includes(key) ? <span className="rs-pm-dirty" aria-label="Unsaved changes" /> : null;

  return <Dialog open={open} onOpenChange={requestOpenChange}>
    <DialogContent size="xl" className="rs-pm">
      <DialogHeader className="rs-pm-header">
        <DialogTitle>{item ? `${item.title} · settings` : 'Title settings'}</DialogTitle>
        {form && <Button disabled={!canSave} onClick={save}>{saving ? 'Saving…' : 'Save'}</Button>}
      </DialogHeader>
      <DialogBody>
        {!item && <p className="rs-pm-muted">Loading…</p>}
        {item && form && <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="details">Details{(edited('title') || edited('description') || edited('categories'))}</TabsTrigger>
            <TabsTrigger value="poster">Poster &amp; backdrop</TabsTrigger>
            <TabsTrigger value="performers">Performers{edited('cast')}</TabsTrigger>
            <TabsTrigger value="studio">Studio{edited('studio')}</TabsTrigger>
            <TabsTrigger value="series">Series</TabsTrigger>
            <TabsTrigger value="reel">Reel</TabsTrigger>
            <TabsTrigger value="match">Match</TabsTrigger>
            <TabsTrigger value="tracks">Audio &amp; subtitles</TabsTrigger>
            <TabsTrigger value="file">File</TabsTrigger>
          </TabsList>
          <div className="rs-pm-panel">
            <TabsContent value="details"><DetailsTab draft={form.draft} update={update} onSubmit={save} files={item.files} /></TabsContent>
            <TabsContent value="poster"><PosterTab item={item} onChanged={changed} /></TabsContent>
            <TabsContent value="performers"><PerformersTab draft={form.draft} update={update} /></TabsContent>
            <TabsContent value="studio"><StudioTab draft={form.draft} update={update} /></TabsContent>
            <TabsContent value="series"><SeriesTab item={item} onChanged={changed} /></TabsContent>
            <TabsContent value="reel"><ReelTab item={item} onChanged={changed} /></TabsContent>
            <TabsContent value="match"><MatchTab item={item} onMatched={changed} /></TabsContent>
            <TabsContent value="tracks">{tab === 'tracks' && <MediaTracksTab item={item} />}</TabsContent>
            <TabsContent value="file"><FileTab item={item} canDelete={canDelete} onDeleted={onDeleted} /></TabsContent>
          </div>
        </Tabs>}
      </DialogBody>
    </DialogContent>
    <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
          <AlertDialogDescription>Your edits to this title have not been saved.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Keep editing</AlertDialogCancel>
          <AlertDialogAction onClick={() => { setConfirmClose(false); onOpenChange(false); }}>Discard</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </Dialog>;
}
