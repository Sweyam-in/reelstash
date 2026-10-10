import React, { useCallback, useEffect, useState } from 'react';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Dialog, DialogBody, DialogContent, DialogHeader, DialogTitle,
  FormField, Input, Tabs, TabsContent, TabsList, TabsTrigger, useToast,
} from '@spatika/react';
import { Combine, ImageUp, Search, Trash2, X } from 'lucide-react';
import {
  deleteStudio, deleteStudioImage, getMediaItems, getStudio, getStudioDuplicates, getStudioMatchJob, getStudios, linkStudioMedia, matchStudioProvider, mergeStudio,
  searchStudioProviders, studioImageUrl, unlinkStudioMedia, unmatchStudioProvider, updateStudio, uploadStudioImage,
} from '../api';
import { PerformerMatch } from './PerformerMatch';
import { TitleThumb } from './PerformerSettings';

function Logo({ studio }) {
  const [failed, setFailed] = useState(false);
  if (studio.logoImageId && !failed) {
    return <img className="rs-pm-avatar" src={studioImageUrl(studio.id, studio.logoImageId)} alt="" onError={() => setFailed(true)} />;
  }
  return <span className="rs-pm-avatar rs-pm-avatar-fallback">{(studio.name || '?').slice(0, 2).toUpperCase()}</span>;
}

function titles(count) {
  return `${count || 0} ${count === 1 ? 'title' : 'titles'}`;
}

function titleMeta(item, studioId) {
  const parts = [item.studioId && item.studioId !== studioId ? item.studioName : null, item.releaseDate?.slice(0, 4)].filter(Boolean);
  return parts.length ? <small>{parts.join(' · ')}</small> : null;
}

function DetailsTab({ studio, onSaved, onDeleted }) {
  const { toast } = useToast();
  const [name, setName] = useState(studio.name || '');
  const [website, setWebsite] = useState(studio.website || '');
  const [description, setDescription] = useState(studio.description || '');
  const [busy, setBusy] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const result = await updateStudio(studio.id, { name: name.trim(), website, description });
      toast({ title: 'Studio saved', description: result.nfoFailed ? `${result.nfoFailed} NFO files could not be updated.` : result.nfoUpdated ? `Updated ${result.nfoUpdated} NFO ${result.nfoUpdated === 1 ? 'file' : 'files'} with the new name.` : undefined, tone: result.nfoFailed ? 'warn' : 'success' });
      onSaved();
    } catch (err) {
      toast({ title: 'Could not save studio', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await deleteStudio(studio.id);
      toast({ title: 'Studio deleted', tone: 'success' });
      onDeleted?.();
    } catch (err) {
      toast({ title: 'Could not delete studio', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  return <form className="rs-pm-form" onSubmit={save}>
    <FormField label="Name"><Input value={name} onChange={e => setName(e.target.value)} /></FormField>
    <FormField label="Website"><Input value={website} onChange={e => setWebsite(e.target.value)} placeholder="Optional" /></FormField>
    <FormField label="Description"><textarea className="rs-pm-textarea" rows={5} value={description} onChange={e => setDescription(e.target.value)} placeholder="Optional" /></FormField>
    <p className="rs-pm-muted">Renaming also rewrites the studio in the NFO file of each of its titles.</p>
    <div><Button type="submit" disabled={busy || !name.trim()}>Save changes</Button></div>
    <div className="rs-pm-danger">
      <p className="rs-eyebrow">Delete studio</p>
      <p className="rs-pm-muted">Its videos are left without a studio. Video files and NFO files are not touched.</p>
      <Button type="button" variant="secondary" disabled={busy} onClick={() => setConfirmRemove(true)}><Trash2 size={15} /> Delete studio</Button>
    </div>
    <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {studio.name}?</AlertDialogTitle>
          <AlertDialogDescription>Its videos are left without a studio. Video files and NFO files are not touched. This can’t be undone.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={remove}>Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </form>;
}

function LogoTab({ studio, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [studio.logoImageId]);

  async function run(action, success) {
    setBusy(true);
    try {
      await action();
      onChanged();
      toast({ title: success, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update logo', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadStudioImage(studio.id, file), 'Logo saved');
  }

  return <div className="rs-pm-photo">
    <div className="rs-pm-photo-frame">
      {studio.logoImageId && !failed
        ? <img className="rs-pm-avatar" src={studioImageUrl(studio.id, studio.logoImageId)} alt="" onError={() => setFailed(true)} />
        : <span className="rs-pm-avatar rs-pm-avatar-fallback">{(studio.name || '?').slice(0, 2).toUpperCase()}</span>}
    </div>
    <div>
      <p className="rs-pm-muted">Upload your own logo. It replaces the current one. JPEG, PNG, WebP or GIF, up to 8 MB.</p>
      <div className="rs-pm-actions">
        <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
          <ImageUp size={16} /> {busy ? 'Working…' : 'Choose image'}
          <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={upload} />
        </label>
        {studio.logoImageId && <Button variant="secondary" disabled={busy} onClick={() => run(() => deleteStudioImage(studio.id), 'Logo removed')}><Trash2 size={15} /> Remove logo</Button>}
      </div>
      <p className="rs-pm-muted">A metadata provider match also fetches a logo. Use the Metadata Providers tab for that.</p>
    </div>
  </div>;
}

function VideosTab({ studio, onChanged }) {
  const { toast } = useToast();
  const [linkedList, setLinkedList] = useState(null);
  const [total, setTotal] = useState(0);
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);
  const [busyId, setBusyId] = useState(null);

  const load = useCallback(() => getMediaItems({ studioId: studio.id, limit: 200, sort: 'title' }).then(result => { setLinkedList(result.items || []); setTotal(result.total ?? (result.items || []).length); }).catch(err => toast({ title: 'Could not load videos', description: err.message, tone: 'danger' })), [studio.id, toast]);
  useEffect(() => { setLinkedList(null); load(); }, [load]);

  async function search(event) {
    event.preventDefault();
    if (!query.trim()) return;
    try {
      const result = await getMediaItems({ q: query.trim(), limit: 12 });
      setFound(result.items || []);
    } catch (err) {
      toast({ title: 'Video search failed', description: err.message, tone: 'danger' });
    }
  }

  async function change(link, item) {
    setBusyId(item.id);
    try {
      const result = link ? await linkStudioMedia(studio.id, item.id) : await unlinkStudioMedia(studio.id, item.id);
      await load();
      setFound(list => list && list.map(row => (row.id === item.id ? { ...row, studioId: link ? studio.id : null, studioName: link ? studio.name : null } : row)));
      onChanged();
      toast({ title: link ? `Added to ${studio.name}` : `Removed from ${studio.name}`, description: result.nfoFailed ? `${result.nfoFailed} NFO ${result.nfoFailed === 1 ? 'file' : 'files'} could not be updated.` : undefined, tone: result.nfoFailed ? 'warn' : 'success' });
    } catch (err) {
      toast({ title: 'Could not update videos', description: err.message, tone: 'danger' });
    } finally {
      setBusyId(null);
    }
  }

  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">Add a video</p>
      <p className="rs-pm-muted">Adding a video that belongs to another studio moves it here. The studio in the video’s NFO file is updated too.</p>
      <form className="rs-match-search" onSubmit={search}>
        <input aria-label="Find a video to add" value={query} onChange={e => setQuery(e.target.value)} placeholder="Search videos" />
        <button className="rs-button rs-button-outline" type="submit" disabled={!query.trim()}><Search size={15} /> Find</button>
      </form>
      {found && !found.length && <p className="rs-pm-muted">No videos match that search.</p>}
      {found && !!found.length && <div className="rs-pm-found">
        {found.map(item => <div key={item.id} className="rs-pm-found-row rs-pm-title-row">
          <TitleThumb item={item} />
          <span><strong>{item.title}</strong>{titleMeta(item, studio.id)}</span>
          {item.studioId === studio.id
            ? <small>In this studio</small>
            : <Button size="sm" variant="secondary" disabled={busyId === item.id} onClick={() => change(true, item)}>{item.studioId ? 'Move here' : 'Add'}</Button>}
        </div>)}
      </div>}
    </section>
    <section>
      <p className="rs-eyebrow">Videos in this studio{linkedList ? ` · ${total}` : ''}</p>
      {!linkedList && <p className="rs-pm-muted">Loading…</p>}
      {linkedList && !linkedList.length && <p className="rs-pm-muted">No videos belong to this studio yet.</p>}
      <div className="rs-pm-found">
        {(linkedList || []).map(item => <div key={item.id} className="rs-pm-found-row rs-pm-title-row">
          <TitleThumb item={item} />
          <span><strong>{item.title}</strong>{titleMeta(item, studio.id)}</span>
          <button type="button" className="rs-pm-icon" disabled={busyId === item.id} aria-label={`Remove ${item.title} from this studio`} title="Remove from studio" onClick={() => change(false, item)}><X size={15} /></button>
        </div>)}
      </div>
      {linkedList && total > linkedList.length && <p className="rs-pm-muted">Showing the first {linkedList.length}. Search above to find a specific video.</p>}
    </section>
  </div>;
}

// Two studios that might be the same. The user picks which name survives; the
// other is merged into it.
function MergePair({ studio, other, reason, onMerged, onCancel }) {
  const { toast } = useToast();
  const [keep, setKeep] = useState(null); // 'self' | 'other'
  const [busy, setBusy] = useState(false);
  const self = { id: studio.id, name: studio.name, logoImageId: studio.logoImageId, mediaCount: studio.mediaCount };
  const survivor = keep === 'self' ? self : other;
  const removed = keep === 'self' ? other : self;

  async function confirm() {
    setBusy(true);
    try {
      const result = await mergeStudio(survivor.id, removed.id);
      onMerged({ keepId: survivor.id, sourceId: removed.id, keepName: survivor.name, sourceName: removed.name, result });
    } catch (err) {
      toast({ title: 'Could not merge studios', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  const side = (key, member) => <div className={`rs-pm-member${keep === key ? ' is-keep' : ''}${key === 'self' ? ' is-current' : ''}`}>
    <Logo studio={member} />
    <div>
      <strong>{member.name}</strong>
      <small>{titles(member.mediaCount)}{key === 'self' ? ' · this studio' : ''}</small>
    </div>
    <Button size="sm" variant="secondary" disabled={busy} onClick={() => setKeep(key)}>Keep this name</Button>
  </div>;

  return <div className="rs-pm-pair">
    {reason && <p className="rs-pm-reason">{reason}</p>}
    <div className="rs-pm-pair-grid">{side('self', self)}<Combine size={18} aria-hidden="true" />{side('other', other)}</div>
    {keep && <div className="rs-pm-confirm">
      <p>Merge <strong>{removed.name}</strong> into <strong>{survivor.name}</strong>? Its videos, series and provider matches move over, {removed.name} becomes an alias, and the NFO files of those videos are rewritten. This can’t be undone.</p>
      <div>
        <Button size="sm" disabled={busy} onClick={confirm}>{busy ? 'Merging…' : 'Merge studios'}</Button>
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => setKeep(null)}>Cancel</Button>
      </div>
    </div>}
    {!keep && onCancel && <button type="button" className="rs-pm-link" onClick={onCancel}>Choose another studio</button>}
  </div>;
}

function MergeTab({ studio, onMerged }) {
  const { toast } = useToast();
  const [suggestions, setSuggestions] = useState(null);
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);
  const [manual, setManual] = useState(null);

  useEffect(() => {
    setSuggestions(null); setManual(null); setFound(null);
    getStudioDuplicates(studio.id).then(result => setSuggestions(result.items || [])).catch(err => { setSuggestions([]); toast({ title: 'Could not look for duplicates', description: err.message, tone: 'danger' }); });
  }, [studio.id, toast]);

  function merged(info) {
    const goneId = info.sourceId === studio.id ? info.keepId : info.sourceId;
    setSuggestions(list => (list || []).filter(item => item.id !== goneId));
    setFound(list => list && list.filter(item => item.id !== goneId));
    setManual(null);
    onMerged(info);
  }

  async function search(event) {
    event.preventDefault();
    if (!query.trim()) return;
    try {
      const result = await getStudios({ q: query.trim(), limit: 8 });
      setFound((result.items || []).filter(item => item.id !== studio.id));
    } catch (err) {
      toast({ title: 'Studio search failed', description: err.message, tone: 'danger' });
    }
  }

  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">Possible duplicates</p>
      {!suggestions && <p className="rs-pm-muted">Looking for duplicates…</p>}
      {suggestions && !suggestions.length && <p className="rs-pm-muted">Nothing else looks like this studio.</p>}
      {!!suggestions?.length && <div className="rs-pm-pairs">
        {suggestions.map(item => <MergePair key={item.id} studio={studio} other={item} reason={item.reason} onMerged={merged} />)}
      </div>}
    </section>
    <section>
      <p className="rs-eyebrow">Merge with another studio</p>
      <p className="rs-pm-muted">Search for the studio to merge if it isn’t listed above.</p>
      <form className="rs-match-search" onSubmit={search}>
        <input aria-label="Find a studio to merge" value={query} onChange={e => setQuery(e.target.value)} placeholder="Search studios" />
        <button className="rs-button rs-button-outline" type="submit" disabled={!query.trim()}><Search size={15} /> Find</button>
      </form>
      {found && !found.length && <p className="rs-pm-muted">No other studios match that search.</p>}
      {found && !!found.length && !manual && <div className="rs-pm-found">
        {found.map(item => <button type="button" key={item.id} className="rs-pm-found-row" onClick={() => setManual(item)}>
          <Logo studio={item} /><span><strong>{item.name}</strong><small>{titles(item.mediaCount)}</small></span>
        </button>)}
      </div>}
      {manual && <MergePair key={manual.id} studio={studio} other={manual} reason="Chosen manually" onMerged={merged} onCancel={() => setManual(null)} />}
    </section>
  </div>;
}

/**
 * The studio settings modal: details, metadata provider search and match, logo,
 * the studio's videos, and merging duplicates. `onSwitch(id)` fires when this
 * studio was merged into another and the page should follow it.
 */
export default function StudioSettings({ studioId, open, onOpenChange, onChanged, onSwitch, onDeleted }) {
  const { toast } = useToast();
  const [studio, setStudio] = useState(null);
  const [tab, setTab] = useState('details');

  const load = useCallback(() => getStudio(studioId).then(result => { setStudio(result); return result; }), [studioId]);
  useEffect(() => {
    if (!open) return;
    setTab('details');
    setStudio(null);
    load().catch(err => {
      toast({ title: 'Could not load studio', description: err.message, tone: 'danger' });
      onOpenChange(false);
    });
  }, [open, load]); // eslint-disable-line react-hooks/exhaustive-deps

  const changed = useCallback(() => load().then(() => onChanged?.()).catch(err => toast({ title: 'Could not refresh studio', description: err.message, tone: 'danger' })), [load, onChanged, toast]);

  function merged({ keepId, sourceId, keepName, sourceName, result }) {
    const nfo = result.nfoFailed ? ` ${result.nfoFailed} NFO files could not be written.` : result.nfoUpdated ? ` Updated ${result.nfoUpdated} NFO ${result.nfoUpdated === 1 ? 'file' : 'files'}.` : '';
    toast({ title: `Merged ${sourceName} into ${keepName}`, description: `Moved ${titles(result.titles)}.${nfo}`, tone: result.nfoFailed ? 'warn' : 'success' });
    onChanged?.();
    if (sourceId === studioId) onSwitch?.(keepId);
    else load().catch(() => {});
  }

  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent size="xl" className="rs-pm is-studio">
      <DialogHeader><DialogTitle>{studio ? `${studio.name} · settings` : 'Studio settings'}</DialogTitle></DialogHeader>
      <DialogBody>
        {!studio && <p className="rs-pm-muted">Loading…</p>}
        {studio && <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="details">Details</TabsTrigger>
            <TabsTrigger value="match">Metadata Providers</TabsTrigger>
            <TabsTrigger value="logo">Logo</TabsTrigger>
            <TabsTrigger value="videos">Videos</TabsTrigger>
            <TabsTrigger value="merge">Duplicates &amp; merge</TabsTrigger>
          </TabsList>
          <div className="rs-pm-panel">
            <TabsContent value="details"><DetailsTab key={`${studio.id}-${studio.name}`} studio={studio} onSaved={changed} onDeleted={() => { onOpenChange(false); onChanged?.(); onDeleted?.(); }} /></TabsContent>
            <TabsContent value="match"><PerformerMatch kind="studio" performer={studio} onUpdated={changed} imageUrl={studioImageUrl} searchRemote={searchStudioProviders} matchRemote={matchStudioProvider} loadJob={getStudioMatchJob} imageField="logoImageId" unmatchRemote={unmatchStudioProvider} allowMarkWrong={false} /></TabsContent>
            <TabsContent value="logo"><LogoTab studio={studio} onChanged={changed} /></TabsContent>
            <TabsContent value="videos"><VideosTab studio={studio} onChanged={changed} /></TabsContent>
            <TabsContent value="merge"><MergeTab studio={studio} onMerged={merged} /></TabsContent>
          </div>
        </Tabs>}
      </DialogBody>
    </DialogContent>
  </Dialog>;
}
