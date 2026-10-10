import React, { useCallback, useEffect, useState } from 'react';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Dialog, DialogBody, DialogContent, DialogHeader, DialogTitle,
  FormField, Input, Tabs, TabsContent, TabsList, TabsTrigger, useToast,
} from '@spatika/react';
import { ArrowDown, ArrowUp, Check, ExternalLink, ImageUp, Search, Trash2, Unlink, X } from 'lucide-react';
import {
  addSeriesMedia, artworkUrl, assignSeriesPart, deleteSeries, deleteSeriesBackdrop, deleteSeriesImage, seriesBackdropUrl, uploadSeriesBackdrop, uploadSeriesImage, setSeriesProviderImage, getMediaItems, getProviders, getSeriesItem, getSeriesParts,
  linkSeriesProvider, orderSeriesMedia, removeSeriesMedia, searchSeriesProviders, unlinkSeriesProvider, updateSeries,
} from '../api';
import { SearchCombobox, Thumb } from '../components/SearchCombobox';
import SeriesCover from '../components/SeriesCover';

const PART_WORD = { movie: 'scene', show: 'episode', collection: 'film' };
const partWord = (kind, count = 2) => `${PART_WORD[kind] || 'part'}${count === 1 ? '' : 's'}`;

function useSeriesVideos(seriesId) {
  const [videos, setVideos] = useState(null);
  const load = useCallback(() => getMediaItems({ seriesId, sort: 'series', limit: 200 })
    .then(result => { setVideos(result.items || []); return result.items || []; }), [seriesId]);
  useEffect(() => { load().catch(() => setVideos([])); }, [load]);
  return [videos, load];
}

/** The titles in the series, in order. Moving one renumbers the series 1, 2, 3… `current` marks the title being edited. */
export function VideosTab({ series, onChanged, current, showAdd = true }) {
  const { toast } = useToast();
  const [videos, reload] = useSeriesVideos(series.id);
  const [busy, setBusy] = useState(false);

  const searchVideos = useCallback(async q => {
    try { return ((await getMediaItems({ q, limit: 24 })).items || []).map(item => ({ ...item, name: item.title })); }
    catch (err) { toast({ title: 'Search failed', description: err.message, tone: 'danger' }); return []; }
  }, [toast]);

  async function run(action, success) {
    setBusy(true);
    try {
      await action();
      if (success) toast({ title: success, tone: 'success' });
      await reload();
      onChanged();
    } catch (err) {
      toast({ title: 'Could not update the series', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function move(index, delta) {
    const next = [...videos];
    const [moved] = next.splice(index, 1);
    next.splice(index + delta, 0, moved);
    run(() => orderSeriesMedia(series.id, next.map(item => item.id)));
  }

  const inSeries = item => !!videos?.some(v => v.id === item.id);
  const numbered = videos?.every((item, index) => item.seriesPosition === index + 1);

  return <div className="rs-pm-merge rs-pm-performers">
    {showAdd && <section>
      <p className="rs-eyebrow">Add a video</p>
      <SearchCombobox
        placeholder="Search your library"
        searchFn={searchVideos}
        keepOpen
        onPick={item => item.id && !inSeries(item) && run(() => addSeriesMedia(series.id, [item.id]), `Added “${item.title}”`)}
        renderOption={item => <><Thumb name={item.title} src={artworkUrl(item.id)} /><span><strong>{item.title}</strong>{inSeries(item) ? <small>Already in this series</small> : item.seriesName && <small>{`In ${item.seriesName}`}</small>}</span></>}
      />
    </section>}
    <section>
      <p className="rs-eyebrow">In this series · {videos?.length ?? '…'}</p>
      {videos && !videos.length && <p className="rs-pm-muted">No videos yet. Search above, or open a video’s Settings and use its Series tab.</p>}
      {videos && videos.length > 1 && !numbered && <p className="rs-pm-muted">Numbers follow the matched scenes. Moving a video renumbers the whole series 1, 2, 3…</p>}
      <div className="rs-pm-found">
        {(videos || []).map((item, index) => <div key={item.id} className={`rs-pm-found-row rs-pm-title-row${item.id === current ? ' is-current' : ''}`}>
          <span className="rs-pm-who">
            <span className="rs-series-number" aria-label={`Number ${item.seriesPosition ?? 'not set'}`}>{item.seriesPosition ?? '–'}</span>
            <Thumb name={item.title} src={artworkUrl(item.id)} />
            <span><strong>{item.title}</strong>{item.id === current ? <small>This video</small> : item.releaseDate && <small>{item.releaseDate.slice(0, 10)}</small>}</span>
          </span>
          <span className="rs-pm-actions">
            <button type="button" className="rs-pm-icon" aria-label={`Move ${item.title} up`} disabled={busy || index === 0} onClick={() => move(index, -1)}><ArrowUp size={15} /></button>
            <button type="button" className="rs-pm-icon" aria-label={`Move ${item.title} down`} disabled={busy || index === videos.length - 1} onClick={() => move(index, 1)}><ArrowDown size={15} /></button>
            <button type="button" className="rs-pm-icon" aria-label={`Remove ${item.title} from the series`} title="Remove from series" disabled={busy} onClick={() => run(() => removeSeriesMedia(series.id, item.id), `Removed “${item.title}”`)}><X size={15} /></button>
          </span>
        </div>)}
      </div>
    </section>
  </div>;
}

/** One linked provider record: its scenes/episodes, each paired with a video of the series. */
function PartsTable({ series, link, videos, providerName, onChanged, onUnlink }) {
  const { toast } = useToast();
  const [choice, setChoice] = useState({}); // part externalId -> chosen media item id
  const [busy, setBusy] = useState(false);
  const [applyDetails, setApplyDetails] = useState(true);
  const [confirmAll, setConfirmAll] = useState(false);
  useEffect(() => setChoice({}), [link]);

  const paired = new Set(link.parts.filter(p => p.mediaItemId).map(p => p.mediaItemId));
  const byId = Object.fromEntries((videos || []).map(v => [v.id, v]));
  const selected = part => choice[part.externalId] ?? part.suggestedMediaItemId ?? '';
  const pending = link.parts.filter(p => !p.mediaItemId && selected(p));

  async function assign(parts) {
    setBusy(true);
    let done = 0;
    try {
      for (const part of parts) {
        await assignSeriesPart(series.id, { mediaItemId: Number(selected(part)), provider: link.provider, externalId: part.externalId, mode: applyDetails ? 'apply' : 'link' });
        done++;
      }
      toast({ title: done === 1 ? `Matched ${parts[0].label || parts[0].title}` : `Matched ${done} ${partWord(link.kind, done)}`, description: applyDetails ? 'Each video was renamed and its NFO and details updated.' : 'Only the links and numbers were recorded.', tone: 'success' });
    } catch (err) {
      toast({ title: done ? `Matched ${done}, then stopped` : 'Could not match', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
      onChanged();
    }
  }

  return <section className="rs-series-link">
    <div className="rs-match-links"><div className="rs-series-link-head">
      <span>Linked to <strong>{link.title || link.externalId}</strong> on <strong>{providerName(link.provider)}</strong> · {link.parts.length} {partWord(link.kind, link.parts.length)}
        {link.sourceUrl && <a className="rs-series-source" href={link.sourceUrl} target="_blank" rel="noopener noreferrer" aria-label="Open on the provider’s site"><ExternalLink size={14} /></a>}</span>
      <button type="button" className="rs-button rs-button-danger" disabled={busy} onClick={onUnlink}><Unlink size={15} /> Unlink</button>
    </div></div>
    {!link.parts.length && <p className="rs-pm-muted">{providerName(link.provider)} lists no {partWord(link.kind)} for this record.</p>}
    {!!link.parts.length && <>
      <label className="rs-pm-check"><input type="checkbox" checked={applyDetails} onChange={e => setApplyDetails(e.target.checked)} /> Apply each {partWord(link.kind, 1)}’s details to its video (renames the file and rewrites its NFO). Off: only link it and set its number.</label>
      {pending.length > 1 && <div><button type="button" className="rs-button rs-button-primary" disabled={busy} onClick={() => setConfirmAll(true)}><Check size={15} /> Match {pending.length} selected {partWord(link.kind)}</button></div>}
      <div className="rs-series-parts" role="table" aria-label={`${partWord(link.kind)} of ${link.title}`}>
        {link.parts.map(part => {
          const video = part.mediaItemId ? byId[part.mediaItemId] : null;
          const value = selected(part);
          return <div key={part.externalId} className={`rs-series-part${part.mediaItemId ? ' is-paired' : ''}`} role="row">
            <span className="rs-series-number" role="cell">{part.number}</span>
            {part.poster ? <img className="rs-pm-thumb" src={part.poster} alt="" loading="lazy" /> : <Thumb name={part.title} />}
            <span className="rs-series-part-copy" role="cell">
              <strong>{part.title || part.label}</strong>
              <small>{[part.label, part.date, (part.performers || []).slice(0, 3).join(', ')].filter(Boolean).join(' · ')}</small>
            </span>
            <span className="rs-series-part-video" role="cell">
              {part.mediaItemId
                ? <span className="rs-series-paired"><Check size={14} /> {video?.title || `Title ${part.mediaItemId}`}</span>
                : <>
                  <select aria-label={`Video for ${part.label || part.title}`} value={value} disabled={busy || !videos?.length} onChange={e => setChoice(c => ({ ...c, [part.externalId]: e.target.value }))}>
                    <option value="">{videos?.length ? 'Choose a video…' : 'Add videos first'}</option>
                    {(videos || []).map(v => <option key={v.id} value={v.id}>{paired.has(v.id) ? '✓ ' : ''}{v.seriesPosition != null ? `${v.seriesPosition}. ` : ''}{v.title}{v.id === part.suggestedMediaItemId ? ' (suggested)' : ''}</option>)}
                  </select>
                  <button type="button" className="rs-button rs-button-outline" disabled={busy || !value} onClick={() => assign([part])}>Match</button>
                </>}
            </span>
          </div>;
        })}
      </div>
    </>}
    <AlertDialog open={confirmAll} onOpenChange={setConfirmAll}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Match {pending.length} {partWord(link.kind)}?</AlertDialogTitle>
          <AlertDialogDescription>{applyDetails
            ? `Each selected video is matched to its ${partWord(link.kind, 1)} on ${providerName(link.provider)}: the file is renamed, its NFO rewritten, and its title, performers and categories replaced. Its number in the series is set too.`
            : `Each selected video is linked to its ${partWord(link.kind, 1)} and numbered. Files and details are not changed.`}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={() => { setConfirmAll(false); assign(pending); }}>Match all</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </section>;
}

function MatchTab({ series, onChanged }) {
  const { toast } = useToast();
  const [allProviders, setAllProviders] = useState([]);
  const [scope, setScope] = useState('all');
  const [query, setQuery] = useState('');
  const [results, setResults] = useState(null);
  const [links, setLinks] = useState(null);
  const [busy, setBusy] = useState(false);
  const [rename, setRename] = useState(true);
  const [videos, reloadVideos] = useSeriesVideos(series.id);

  useEffect(() => { getProviders().then(r => setAllProviders(r.items || [])).catch(() => setAllProviders([])); }, []);
  const providers = allProviders.filter(p => p.enabled && (p.kinds.includes('series') || p.kinds.includes('media')));
  const providerName = id => allProviders.find(p => p.id === id)?.name || id;
  const scopeLabel = scope === 'all' ? 'all providers' : providerName(scope);

  const loadLinks = useCallback(() => getSeriesParts(series.id).then(r => setLinks(r.items || [])).catch(() => setLinks([])), [series.id]);
  useEffect(() => { loadLinks(); }, [loadLinks]);

  async function search(q, searchScope = scope) {
    setBusy(true);
    try {
      const result = await searchSeriesProviders(series.id, q, searchScope === 'all' ? [] : [searchScope]);
      setResults(result.items || []);
      if (!q) setQuery(result.query || '');
    } catch (err) {
      toast({ title: 'Search failed', description: err.message, tone: 'danger' });
      setResults(null);
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => { setResults(null); setQuery(series.name); }, [series.id]); // eslint-disable-line react-hooks/exhaustive-deps

  async function link(candidate, parent = false) {
    setBusy(true);
    try {
      const result = await linkSeriesProvider(series.id, candidate.provider, candidate.externalId, rename, parent);
      toast({ title: `Linked to “${result.title}”`, description: `${result.partCount} ${result.partCount === 1 ? 'part' : 'parts'} found. Pair them with this series’ videos below.`, tone: 'success' });
      setResults(null);
      await loadLinks();
      onChanged();
    } catch (err) {
      toast({ title: 'Could not link the series', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function unlink(providerId) {
    setBusy(true);
    try {
      await unlinkSeriesProvider(series.id, providerId);
      toast({ title: `Unlinked ${providerName(providerId)}`, description: 'The videos keep their own matches.', tone: 'success' });
      await loadLinks();
      onChanged();
    } catch (err) {
      toast({ title: 'Could not unlink', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  const refresh = () => { loadLinks(); reloadVideos(); onChanged(); };

  if (allProviders.length && !providers.length) {
    return <p className="rs-pm-muted">No metadata provider is configured. Add a TMDB key under Metadata → Providers, or install a provider plugin under Plugins.</p>;
  }
  return <div className="rs-pm-merge">
    <p className="rs-pm-muted">Search your metadata providers for the series, or for any one of its scenes, episodes or films. A movie brings its scenes (from a provider plugin), a show its episodes and a collection its films (TMDB). A single title can be used as it is, as the series’ own details, or for the movie, show or collection it belongs to. Then pair each part with one of the series’ videos.</p>
    {(links || []).map(l => <PartsTable key={l.provider} series={series} link={l} videos={videos} providerName={providerName} onChanged={refresh} onUnlink={() => unlink(l.provider)} />)}
    {providers.length > 1 && <div className="rs-match-providers" role="group" aria-label="Search providers">
      {[{ id: 'all', name: 'All' }, ...providers].map(p => <button key={p.id} type="button" className={`rs-button ${scope === p.id ? 'rs-button-primary' : 'rs-button-outline'}`} aria-pressed={scope === p.id} disabled={busy} onClick={() => { setScope(p.id); if (query.trim()) search(query.trim(), p.id); }}>{p.name}</button>)}
    </div>}
    <form className="rs-match-search" onSubmit={event => { event.preventDefault(); search(query.trim()); }}>
      <input aria-label={`Search ${scopeLabel}`} value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${scopeLabel} for a series or any of its titles`} />
      <button className="rs-button rs-button-outline" type="submit" disabled={busy}><Search size={15} /> {busy ? 'Searching…' : 'Search'}</button>
    </form>
    {results && <label className="rs-pm-check"><input type="checkbox" checked={rename} onChange={e => setRename(e.target.checked)} /> Rename this series to the provider’s title</label>}
    {results && !results.length && <p className="rs-pm-muted">No results for that search.</p>}
    {!!results?.length && <div className="rs-performer-candidates">
      {results.map(candidate => {
        const linked = (links || []).some(l => l.provider === candidate.provider && l.externalId === candidate.externalId);
        return <div key={`${candidate.provider}:${candidate.externalId}`} className="rs-performer-candidate">
          {candidate.poster ? <img src={candidate.poster} alt="" loading="lazy" /> : <span className="rs-directory-mark">{candidate.title.slice(0, 2).toUpperCase()}</span>}
          <span>
            <strong>{candidate.title}</strong>
            <small>{[providers.length > 1 && providerName(candidate.provider), candidate.kind, candidate.studio, candidate.date?.slice(0, 4), candidate.partCount ? `${candidate.partCount} ${partWord(candidate.kind, candidate.partCount)}` : null].filter(Boolean).join(' · ')}</small>
            {linked && <small>Currently linked</small>}
          </span>
          <span className="rs-match-actions">
            <button type="button" className="rs-button rs-button-outline" disabled={busy} title={candidate.isSeries ? undefined : 'Use this title’s details for the series; it becomes the series’ one part'} onClick={() => link(candidate)}>{linked ? 'Refresh' : candidate.isSeries ? 'Link series' : 'Use this title'}</button>
            {!candidate.isSeries && <button type="button" className="rs-button rs-button-outline" disabled={busy} title="Link the movie, show or collection this title belongs to, with all its parts" onClick={() => link(candidate, true)}>Use its {candidate.provider === 'tmdb' ? (candidate.kind === 'episode' ? 'show' : 'collection') : 'movie'}</button>}
          </span>
        </div>;
      })}
    </div>}
  </div>;
}

/** The series poster: upload one, take one from a linked provider, or fall back to a collage of the videos' thumbnails. */
function PosterTab({ series, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [allProviders, setAllProviders] = useState([]);
  useEffect(() => { getProviders().then(r => setAllProviders(r.items || [])).catch(() => setAllProviders([])); }, []);
  const providerName = id => allProviders.find(p => p.id === id)?.name || id;
  const posters = Object.entries(series.providerPosters || {});

  async function run(action, success) {
    setBusy(true);
    try {
      await action();
      await onChanged();
      toast({ title: success, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update the poster', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadSeriesImage(series.id, file), 'Poster saved');
  }

  const source = !series.posterImageId ? 'Showing thumbnails from the series’ videos.' : series.posterCustom ? 'Using a poster you chose.' : 'Using the poster from the linked provider.';
  return <div className="rs-series-poster-panel">
    <span className="rs-series-poster-frame"><SeriesCover series={series} /></span>
    <div>
      <p className="rs-pm-muted">{source}</p>
      <div className="rs-pm-actions">
        <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
          <ImageUp size={16} /> {busy ? 'Working…' : 'Upload poster'}
          <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={upload} />
        </label>
        {series.posterImageId && <Button variant="secondary" disabled={busy} onClick={() => run(() => deleteSeriesImage(series.id), 'Poster removed')}><Trash2 size={15} /> Use video thumbnails</Button>}
      </div>
      <p className="rs-pm-muted">JPEG, PNG, WebP or GIF, up to 8 MB. Without a poster the series shows a collage of its first videos, like a playlist.</p>
      {!!posters.length && <>
        <p className="rs-eyebrow">From linked providers</p>
        <div className="rs-series-provider-posters">
          {posters.map(([providerId, url]) => <button key={providerId} type="button" className="rs-series-provider-poster" disabled={busy} onClick={() => run(() => setSeriesProviderImage(series.id, providerId), `Using the ${providerName(providerId)} poster`)}>
            <img src={url} alt="" loading="lazy" />
            <span>Use {providerName(providerId)} poster</span>
          </button>)}
        </div>
      </>}
      {!posters.length && <p className="rs-pm-muted">Link the series on the Metadata Providers tab to pick a provider poster.</p>}
    </div>
  </div>;
}

function BackdropTab({ series, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  async function run(action, success) {
    setBusy(true);
    try {
      await action();
      await onChanged();
      toast({ title: success, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update the backdrop', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }
  function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadSeriesBackdrop(series.id, file), 'Backdrop saved');
  }
  return <div className="rs-series-backdrop-panel">
    <span className="rs-series-backdrop-frame">
      {series.backdropImageId ? <img src={seriesBackdropUrl(series.id, series.backdropImageId)} alt="" /> : <span className="rs-pm-muted">No backdrop</span>}
    </span>
    <div>
      <p className="rs-pm-muted">{series.backdropImageId ? 'Shown, blurred and tinted, behind the series page header.' : 'Without one, the series page uses the artwork of its first video.'}</p>
      <div className="rs-pm-actions">
        <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
          <ImageUp size={16} /> {busy ? 'Working…' : series.backdropImageId ? 'Replace backdrop' : 'Upload backdrop'}
          <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={upload} />
        </label>
        {series.backdropImageId && <Button variant="secondary" disabled={busy} onClick={() => run(() => deleteSeriesBackdrop(series.id), 'Backdrop removed')}><Trash2 size={15} /> Remove</Button>}
      </div>
      <p className="rs-pm-muted">JPEG, PNG, WebP or GIF, up to 8 MB. A wide image (16:9) works best.</p>
    </div>
  </div>;
}

function DetailsTab({ series, onSaved, onDeleted }) {
  const { toast } = useToast();
  const [name, setName] = useState(series.name || '');
  const [description, setDescription] = useState(series.description || '');
  const [busy, setBusy] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    try {
      await updateSeries(series.id, { name: name.trim(), description, studioId: series.studioId ?? null });
      toast({ title: 'Series saved', tone: 'success' });
      onSaved();
    } catch (err) {
      toast({ title: 'Could not save series', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await deleteSeries(series.id);
      toast({ title: 'Series deleted', description: 'Its videos stay in your library.', tone: 'success' });
      onDeleted?.();
    } catch (err) {
      toast({ title: 'Could not delete series', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  return <form className="rs-pm-form" onSubmit={save}>
    <FormField label="Name"><Input value={name} onChange={e => setName(e.target.value)} /></FormField>
    <FormField label="Description"><textarea className="rs-pm-textarea" rows={5} value={description} onChange={e => setDescription(e.target.value)} placeholder="Optional" /></FormField>
    <div className="rs-pm-actions">
      <Button type="submit" disabled={busy || !name.trim()}>Save changes</Button>
      <Button type="button" variant="secondary" disabled={busy} onClick={() => setConfirmDelete(true)}><Trash2 size={15} /> Delete series</Button>
    </div>
    <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete “{series.name}”?</AlertDialogTitle>
          <AlertDialogDescription>The series and its provider links are removed. Its videos stay in your library with their own matches.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction onClick={remove}>Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </form>;
}

/** The series settings modal: its videos in order, provider matching with scene/episode pairing, and details. */
export default function SeriesSettings({ seriesId, open, onOpenChange, onChanged, onDeleted }) {
  const { toast } = useToast();
  const [series, setSeries] = useState(null);
  const [tab, setTab] = useState('details');

  const load = useCallback(() => getSeriesItem(seriesId).then(result => { setSeries(result); return result; }), [seriesId]);
  useEffect(() => {
    if (!open) return;
    setTab('details');
    setSeries(null);
    load().catch(err => {
      toast({ title: 'Could not load series', description: err.message, tone: 'danger' });
      onOpenChange(false);
    });
  }, [open, load]); // eslint-disable-line react-hooks/exhaustive-deps

  const changed = useCallback(() => load().then(() => onChanged?.()).catch(() => {}), [load, onChanged]);

  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent size="xl" className="rs-pm">
      <DialogHeader><DialogTitle>{series ? `${series.name} · settings` : 'Series settings'}</DialogTitle></DialogHeader>
      <DialogBody>
        {!series && <p className="rs-pm-muted">Loading…</p>}
        {series && <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="details">Details</TabsTrigger>
            <TabsTrigger value="videos">Videos</TabsTrigger>
            <TabsTrigger value="match">Metadata Providers</TabsTrigger>
            <TabsTrigger value="poster">Poster</TabsTrigger>
            <TabsTrigger value="backdrop">Backdrop</TabsTrigger>
          </TabsList>
          <div className="rs-pm-panel">
            <TabsContent value="videos"><VideosTab series={series} onChanged={changed} /></TabsContent>
            <TabsContent value="match"><MatchTab series={series} onChanged={changed} /></TabsContent>
            <TabsContent value="poster"><PosterTab series={series} onChanged={changed} /></TabsContent>
            <TabsContent value="backdrop"><BackdropTab series={series} onChanged={changed} /></TabsContent>
            <TabsContent value="details"><DetailsTab key={`${series.id}-${series.name}`} series={series} onSaved={changed} onDeleted={() => { onOpenChange(false); onDeleted?.(); }} /></TabsContent>
          </div>
        </Tabs>}
      </DialogBody>
    </DialogContent>
  </Dialog>;
}
