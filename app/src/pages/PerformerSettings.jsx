import React, { useCallback, useEffect, useState } from 'react';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Dialog, DialogBody, DialogContent, DialogHeader, DialogTitle,
  FormField, Input, Tabs, TabsContent, TabsList, TabsTrigger, useToast,
} from '@spatika/react';
import { Check, Combine, ImageUp, RefreshCw, Search, Trash2, X } from 'lucide-react';
import {
  artworkUrl, choosePerformerPhoto, getPerformerFieldSources, setPerformerFieldSources, createPerformer, deletePerformerPhoto, fetchPerformerPhotos, getPerformerPhotos, getProviders, performerPhotoUrl, dismissPerformerDuplicate, getMediaItems, getPerformer, getPerformerDuplicates, getPerformerMedia, getPerformers,
  deletePerformer, linkPerformerMedia, mergePerformer, unlinkPerformerMedia, unmatchPerformerProvider, performerImageUrl, updatePerformer, uploadPerformerImage,
} from '../api';
import { PerformerMatch } from './PerformerMatch';

function Avatar({ member }) {
  const [failed, setFailed] = useState(false);
  if (member.profileImageId && !failed) {
    return <img className="rs-pm-avatar" src={performerImageUrl(member.id, member.profileImageId)} alt="" onError={() => setFailed(true)} />;
  }
  return <span className="rs-pm-avatar rs-pm-avatar-fallback">{(member.name || '?').slice(0, 2).toUpperCase()}</span>;
}

function titles(count) {
  return `${count || 0} ${count === 1 ? 'title' : 'titles'}`;
}

function mergeSummary(result) {
  const parts = [`Moved ${titles(result.titles)}`];
  parts.push(result.nfoUpdated ? `updated ${result.nfoUpdated} NFO ${result.nfoUpdated === 1 ? 'file' : 'files'}` : 'no NFO files needed changes');
  return `${parts.join(', ')}.`;
}

/**
 * Two performers that might be the same person. The user picks which record
 * survives; the other is merged into it (titles, aliases, photo, provider matches
 * and NFO sidecars all follow).
 */
export function DuplicatePairRow({ pair, currentId, onDone, onDismiss }) {
  const { toast } = useToast();
  const [keep, setKeep] = useState(null); // 'a' | 'b'
  const [busy, setBusy] = useState(false);
  const survivor = keep && pair[keep];
  const removed = keep && pair[keep === 'a' ? 'b' : 'a'];

  async function confirm() {
    setBusy(true);
    try {
      const result = await mergePerformer(survivor.id, removed.id);
      onDone({ keepId: survivor.id, sourceId: removed.id, result, keepName: survivor.name, sourceName: removed.name });
    } catch (err) {
      toast({ title: 'Could not merge performers', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  const side = key => {
    const member = pair[key];
    return <div className={`rs-pm-member${keep === key ? ' is-keep' : ''}${member.id === currentId ? ' is-current' : ''}`}>
      <Avatar member={member} />
      <div>
        <strong>{member.name}</strong>
        <small>{titles(member.mediaCount)}{member.id === currentId ? ' · this performer' : ''}</small>
        {!!member.aliases?.length && <small>aka {member.aliases.slice(0, 3).join(', ')}</small>}
      </div>
      <Button size="sm" variant="secondary" disabled={busy} onClick={() => setKeep(key)}>Keep this name</Button>
    </div>;
  };

  return <div className="rs-pm-pair">
    <p className="rs-pm-reason">{pair.reason}</p>
    <div className="rs-pm-pair-grid">{side('a')}<Combine size={18} aria-hidden="true" />{side('b')}</div>
    {keep && <div className="rs-pm-confirm">
      <p>Merge <strong>{removed.name}</strong> into <strong>{survivor.name}</strong>? Its titles move over, {removed.name} becomes an alias, and the NFO files of those titles are rewritten. This can’t be undone.</p>
      <div>
        <Button size="sm" disabled={busy} onClick={confirm}>{busy ? 'Merging…' : 'Merge performers'}</Button>
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => setKeep(null)}>Cancel</Button>
      </div>
    </div>}
    {!keep && onDismiss && <button type="button" className="rs-pm-link" onClick={() => onDismiss(pair)}>Not the same person</button>}
  </div>;
}

export function DuplicateList({ pairs, currentId, onDone, onDismiss, empty }) {
  if (!pairs) return <p className="rs-pm-muted">Looking for duplicates…</p>;
  if (!pairs.length) return <p className="rs-pm-muted">{empty}</p>;
  return <div className="rs-pm-pairs">
    {pairs.map(pair => <DuplicatePairRow key={`${pair.a.id}-${pair.b.id}`} pair={pair} currentId={currentId} onDone={onDone} onDismiss={onDismiss} />)}
  </div>;
}

function DetailsTab({ performer, onSaved, onDeleted }) {
  const { toast } = useToast();
  const [name, setName] = useState(performer.name || '');
  const [displayName, setDisplayName] = useState(performer.displayName || '');
  const [notes, setNotes] = useState(performer.notes || '');
  const [busy, setBusy] = useState(false);
  const [confirmRemove, setConfirmRemove] = useState(false);

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const result = await updatePerformer(performer.id, { name, displayName, notes });
      toast({ title: 'Performer saved', description: result.nfoFailed?.length ? `${result.nfoFailed.length} NFO files could not be updated.` : result.nfoUpdated ? `Updated ${result.nfoUpdated} NFO ${result.nfoUpdated === 1 ? 'file' : 'files'} with the new name.` : undefined, tone: result.nfoFailed?.length ? 'warn' : 'success' });
      onSaved();
    } catch (err) {
      toast({ title: 'Could not save performer', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    setBusy(true);
    try {
      await deletePerformer(performer.id);
      toast({ title: 'Performer deleted', tone: 'success' });
      onDeleted?.();
    } catch (err) {
      toast({ title: 'Could not delete performer', description: err.message, tone: 'danger' });
      setBusy(false);
    }
  }

  return <form className="rs-pm-form" onSubmit={save}>
    <FormField label="Name"><Input value={name} onChange={e => setName(e.target.value)} /></FormField>
    <FormField label="Display name"><Input value={displayName} onChange={e => setDisplayName(e.target.value)} placeholder="Optional" /></FormField>
    <FormField label="Description"><textarea className="rs-pm-textarea" rows={6} value={notes} placeholder="Shown on the performer page. Leave empty to show the bio from your metadata providers, if there is one." onChange={e => setNotes(e.target.value)} /></FormField>
    {!!performer.aliases?.length && <div><p className="rs-eyebrow">Also known as</p><p className="rs-pm-chips">{performer.aliases.map(alias => <span key={alias}>{alias}</span>)}</p></div>}
    <div><Button type="submit" disabled={busy || !name.trim()}>Save changes</Button></div>
    <div className="rs-pm-danger">
      <p className="rs-eyebrow">Delete performer</p>
      <p className="rs-pm-muted">The performer is removed from all of their titles. Video files and NFO files are not touched.</p>
      <Button type="button" variant="secondary" disabled={busy} onClick={() => setConfirmRemove(true)}><Trash2 size={15} /> Delete performer</Button>
    </div>
    <AlertDialog open={confirmRemove} onOpenChange={setConfirmRemove}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {performer.name}?</AlertDialogTitle>
          <AlertDialogDescription>The performer is removed from all of their titles. Video files and NFO files are not touched. This can’t be undone.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={remove}>Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </form>;
}

// A small 16:9 frame of the title, so similar names are easy to tell apart.
export function TitleThumb({ item }) {
  const [failed, setFailed] = useState(false);
  return <span className="rs-pm-title-thumb" aria-hidden="true">
    {!failed && <img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={() => setFailed(true)} />}
  </span>;
}

function TitlesTab({ performer, onChanged }) {
  const { toast } = useToast();
  const [titlesList, setTitlesList] = useState(null);
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);

  const load = useCallback(() => getPerformerMedia(performer.id).then(result => setTitlesList(result.items || [])).catch(err => toast({ title: 'Could not load titles', description: err.message, tone: 'danger' })), [performer.id, toast]);
  useEffect(() => { setTitlesList(null); load(); }, [load]);

  async function search(event) {
    event.preventDefault();
    if (!query.trim()) return;
    try {
      const result = await getMediaItems({ q: query.trim(), limit: 10 });
      setFound(result.items || []);
    } catch (err) {
      toast({ title: 'Title search failed', description: err.message, tone: 'danger' });
    }
  }

  async function change(action, item) {
    try {
      await action(performer.id, item.id);
      await load();
      onChanged();
      toast({ title: action === linkPerformerMedia ? 'Title linked' : 'Title unlinked', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update titles', description: err.message, tone: 'danger' });
    }
  }

  const linked = new Set((titlesList || []).map(item => item.id));
  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">Link another title</p>
      <p className="rs-pm-muted">Linking also adds the performer to the title’s NFO file. Unlinking removes them.</p>
      <form className="rs-match-search" onSubmit={search}>
        <input aria-label="Find a title to link" value={query} onChange={e => setQuery(e.target.value)} placeholder="Search titles" />
        <button className="rs-button rs-button-outline" type="submit" disabled={!query.trim()}><Search size={15} /> Find</button>
      </form>
      {found && !found.length && <p className="rs-pm-muted">No titles match that search.</p>}
      {found && !!found.length && <div className="rs-pm-found">
        {found.map(item => <div key={item.id} className="rs-pm-found-row rs-pm-title-row">
          <TitleThumb item={item} />
          <span><strong>{item.title}</strong>{[item.studioName, item.releaseDate?.slice(0, 4)].filter(Boolean).length ? <small>{[item.studioName, item.releaseDate?.slice(0, 4)].filter(Boolean).join(' · ')}</small> : null}</span>
          {linked.has(item.id) ? <small>Linked</small> : <Button size="sm" variant="secondary" onClick={() => change(linkPerformerMedia, item)}>Link</Button>}
        </div>)}
      </div>}
    </section>
    <section>
      <p className="rs-eyebrow">Linked titles{titlesList ? ` · ${titlesList.length}` : ''}</p>
      {!titlesList && <p className="rs-pm-muted">Loading…</p>}
      {titlesList && !titlesList.length && <p className="rs-pm-muted">No titles are linked to this performer yet.</p>}
      <div className="rs-pm-found">
        {(titlesList || []).map(item => <div key={item.id} className="rs-pm-found-row rs-pm-title-row">
          <TitleThumb item={item} />
          <span><strong>{item.title}</strong>{[item.studioName, item.releaseDate?.slice(0, 4)].filter(Boolean).length ? <small>{[item.studioName, item.releaseDate?.slice(0, 4)].filter(Boolean).join(' · ')}</small> : null}</span>
          <button type="button" className="rs-pm-icon" aria-label={`Unlink ${item.title}`} title="Unlink" onClick={() => change(unlinkPerformerMedia, item)}><X size={15} /></button>
        </div>)}
      </div>
    </section>
  </div>;
}

// When a performer is linked to several providers, pick which one each detail comes from.
function FieldSources({ performer, onChanged }) {
  const { toast } = useToast();
  const [data, setData] = useState(null);
  const [names, setNames] = useState({});
  const [busy, setBusy] = useState(false);

  useEffect(() => { getPerformerFieldSources(performer.id).then(setData).catch(() => setData(null)); }, [performer.id]);
  useEffect(() => { getProviders().then(r => setNames(Object.fromEntries((r.items || []).map(p => [p.id, p.name])))).catch(() => {}); }, []);
  if (!data || data.providers.length < 2) return null;
  const name = id => names[id] || id;
  // Only fields at least one provider has a value for.
  const fields = data.fields.filter(f => data.providers.some(p => data.values[p]?.[f.key]));
  if (!fields.length) return null;

  async function choose(key, providerId) {
    const next = { ...data.sources };
    if (providerId) next[key] = providerId; else delete next[key];
    setBusy(true);
    try {
      setData(await setPerformerFieldSources(performer.id, next));
      onChanged();
    } catch (err) {
      toast({ title: 'Could not save', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  const clip = value => (value.length > 90 ? `${value.slice(0, 90)}…` : value);
  return <section className="rs-field-sources">
    <p className="rs-eyebrow">Details from each provider</p>
    <p className="rs-pm-muted">Choose which provider each detail comes from. Automatic uses the most recent match and fills gaps from the others.</p>
    <div className="rs-field-sources-table" role="table" aria-label="Detail sources">
      <div role="row" className="rs-field-sources-head" style={{ '--cols': data.providers.length }}>
        <span role="columnheader">Detail</span>
        <span role="columnheader">Automatic</span>
        {data.providers.map(p => <span key={p} role="columnheader">{name(p)}</span>)}
      </div>
      {fields.map(field => {
        const chosen = data.sources[field.key] || '';
        return <div key={field.key} role="row" className="rs-field-sources-row" style={{ '--cols': data.providers.length }}>
          <span role="rowheader">{field.label}</span>
          {['', ...data.providers].map(p => {
            const value = p ? data.values[p]?.[field.key] : null;
            const disabled = busy || (p && !value);
            return <label key={p || 'auto'} role="cell" className={`rs-field-source${chosen === p ? ' is-chosen' : ''}${disabled && p ? ' is-empty' : ''}`}>
              <input type="radio" name={`src-${field.key}`} checked={chosen === p} disabled={disabled} onChange={() => choose(field.key, p)}
                aria-label={p ? `${field.label} from ${name(p)}${value ? `: ${clip(value)}` : ' (none)'}` : `${field.label}: automatic`} />
              <span>{p ? (value ? clip(value) : '—') : 'Automatic'}</span>
            </label>;
          })}
        </div>;
      })}
    </div>
  </section>;
}

const SOURCE_LABEL = { upload: 'Uploaded', provider: 'Provider' };

function PhotoTab({ performer, onChanged }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [photos, setPhotos] = useState(null);
  const [providerNames, setProviderNames] = useState({});
  const [confirmDelete, setConfirmDelete] = useState(null);

  const load = useCallback(() => getPerformerPhotos(performer.id).then(r => setPhotos(r.items || [])).catch(() => setPhotos([])), [performer.id]);
  useEffect(() => { load(); }, [load]);
  useEffect(() => { getProviders().then(r => setProviderNames(Object.fromEntries((r.items || []).map(p => [p.id, p.name])))).catch(() => {}); }, []);
  const sourceLabel = source => SOURCE_LABEL[source] || providerNames[source] || source;
  const linked = (performer.links || []).length > 0;

  async function run(action, success) {
    setBusy(true);
    try {
      const result = await action();
      await load();
      onChanged();
      if (success) toast({ tone: 'success', ...(typeof success === 'function' ? success(result) : { title: success }) });
    } catch (err) {
      toast({ title: 'Something went wrong', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function upload(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (file) run(() => uploadPerformerImage(performer.id, file), 'Photo uploaded and shown');
  }

  return <div className="rs-pm-gallery-wrap">
    <p className="rs-pm-muted">Every photo from your metadata providers and your uploads is kept here. Choose the one to show; automatic matches never switch away from a photo you picked.</p>
    <div className="rs-pm-actions">
      <label className={`rs-button rs-button-outline${busy ? ' is-disabled' : ''}`}>
        <ImageUp size={16} /> {busy ? 'Working…' : 'Upload photo'}
        <input type="file" accept="image/jpeg,image/png,image/webp,image/gif" hidden disabled={busy} onChange={upload} />
      </label>
      {linked && <Button variant="secondary" disabled={busy} onClick={() => run(() => fetchPerformerPhotos(performer.id), r => ({ title: r.added ? `Added ${r.added} ${r.added === 1 ? 'photo' : 'photos'}` : 'No new photos', description: r.added ? undefined : 'The linked providers have no photo you don’t already have.' }))}>
        <RefreshCw size={15} /> Get photos from providers
      </Button>}
    </div>
    {photos && !photos.length && <p className="rs-pm-muted">No photos yet. Upload one{linked ? ', or get them from the linked providers' : ', or match this performer on the Metadata Providers tab'}.</p>}
    {!!photos?.length && <ul className="rs-pm-gallery">
      {photos.map(photo => <li key={photo.id} className={photo.primary ? 'is-primary' : undefined}>
        <button type="button" className="rs-pm-gallery-pick" disabled={busy || photo.primary} aria-pressed={photo.primary}
          aria-label={photo.primary ? `${sourceLabel(photo.source)} photo, shown` : `Show the ${sourceLabel(photo.source)} photo`}
          onClick={() => run(() => choosePerformerPhoto(performer.id, photo.id), 'Photo shown')}>
          <img src={performerPhotoUrl(performer.id, photo.id)} alt="" loading="lazy" />
          {photo.primary && <span className="rs-pm-gallery-badge"><Check size={13} /> Shown</span>}
        </button>
        <div className="rs-pm-gallery-meta">
          <span>{sourceLabel(photo.source)}</span>
          <button type="button" className="rs-pm-gallery-delete" disabled={busy} aria-label={`Delete the ${sourceLabel(photo.source)} photo`} onClick={() => setConfirmDelete(photo)}><Trash2 size={14} /></button>
        </div>
      </li>)}
    </ul>}
    <AlertDialog open={!!confirmDelete} onOpenChange={next => { if (!next) setConfirmDelete(null); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete this photo?</AlertDialogTitle>
          <AlertDialogDescription>{confirmDelete?.primary ? 'It is the photo being shown, so the next one in the gallery takes its place. ' : ''}{confirmDelete?.source === 'upload' ? 'The uploaded file is deleted.' : 'You can get it back later with “Get photos from providers”.'}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={() => { const photo = confirmDelete; setConfirmDelete(null); run(() => deletePerformerPhoto(performer.id, photo.id), 'Photo deleted'); }}>Delete</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </div>;
}

function MergeTab({ performer, onMerged }) {
  const { toast } = useToast();
  const [pairs, setPairs] = useState(null);
  const [query, setQuery] = useState('');
  const [found, setFound] = useState(null);
  const [manual, setManual] = useState(null);

  const loadPairs = useCallback(() => {
    getPerformerDuplicates(performer.id).then(result => setPairs(result.items || [])).catch(err => { setPairs([]); toast({ title: 'Could not look for duplicates', description: err.message, tone: 'danger' }); });
  }, [performer.id, toast]);
  useEffect(() => { setPairs(null); setManual(null); setFound(null); loadPairs(); }, [loadPairs]);

  async function search(event) {
    event.preventDefault();
    if (!query.trim()) return;
    try {
      const result = await getPerformers({ q: query.trim(), limit: 8 });
      setFound((result.items || []).filter(item => item.id !== performer.id));
    } catch (err) {
      toast({ title: 'Performer search failed', description: err.message, tone: 'danger' });
    }
  }

  async function dismiss(pair) {
    try {
      await dismissPerformerDuplicate(pair.a.id, pair.b.id);
      setPairs(list => (list || []).filter(item => item !== pair));
      toast({ title: 'Possible duplicate dismissed', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not dismiss duplicate', description: err.message, tone: 'danger' });
    }
  }

  const self = { id: performer.id, name: performer.name, mediaCount: performer.mediaCount, profileImageId: performer.profileImageId, aliases: performer.aliases };
  return <div className="rs-pm-merge">
    <section>
      <p className="rs-eyebrow">Possible duplicates</p>
      <DuplicateList pairs={pairs} currentId={performer.id} onDone={onMerged} onDismiss={dismiss} empty="Nothing else looks like this performer." />
    </section>
    <section>
      <p className="rs-eyebrow">Merge with another performer</p>
      <p className="rs-pm-muted">Search by any spelling or alias if the match isn’t listed above.</p>
      <form className="rs-match-search" onSubmit={search}>
        <input aria-label="Find a performer to merge" value={query} onChange={e => setQuery(e.target.value)} placeholder="Search performers" />
        <button className="rs-button rs-button-outline" type="submit" disabled={!query.trim()}><Search size={15} /> Find</button>
      </form>
      {found && !found.length && <p className="rs-pm-muted">No other performers match that search.</p>}
      {found && !!found.length && !manual && <div className="rs-pm-found">
        {found.map(item => <button type="button" key={item.id} className="rs-pm-found-row" onClick={() => setManual({ a: self, b: item, reason: 'Chosen manually' })}>
          <Avatar member={item} /><span><strong>{item.name}</strong><small>{titles(item.mediaCount)}</small></span>
        </button>)}
      </div>}
      {manual && <DuplicatePairRow pair={manual} currentId={performer.id} onDone={onMerged} />}
      {manual && <button type="button" className="rs-pm-link" onClick={() => setManual(null)}>Choose someone else</button>}
    </section>
  </div>;
}

/**
 * The performer settings modal: metadata provider search and match, photo upload,
 * details, and merging duplicates. `onChanged` fires after any edit so the
 * page behind can refresh; `onSwitch(id)` fires when this performer was merged
 * into another and the page should follow it.
 */
export default function PerformerSettings({ performerId, open, onOpenChange, onChanged, onSwitch, onDeleted, initialTab = 'details' }) {
  const { toast } = useToast();
  const [performer, setPerformer] = useState(null);
  const [tab, setTab] = useState(initialTab);

  const load = useCallback(() => getPerformer(performerId).then(result => { setPerformer(result); return result; }), [performerId]);
  useEffect(() => {
    if (!open) return;
    setTab(initialTab);
  }, [open, initialTab]);
  useEffect(() => {
    if (!open) return;
    setPerformer(null);
    load().catch(err => {
      toast({ title: 'Could not load performer', description: err.message, tone: 'danger' });
      onOpenChange(false);
    });
  }, [open, load]); // eslint-disable-line react-hooks/exhaustive-deps

  const changed = useCallback(() => load().then(() => onChanged?.()).catch(err => toast({ title: 'Could not refresh performer', description: err.message, tone: 'danger' })), [load, onChanged, toast]);

  function merged({ keepId, sourceId, result, keepName, sourceName }) {
    toast({ title: `Merged ${sourceName} into ${keepName}`, description: `${mergeSummary(result)}${result.nfoFailed?.length ? ` ${result.nfoFailed.length} NFO files could not be written.` : ''}`, tone: result.nfoFailed?.length ? 'warn' : 'success' });
    onChanged?.();
    if (sourceId === performerId) onSwitch?.(keepId);
    else load().catch(() => {});
  }

  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent size="xl" className="rs-pm">
      <DialogHeader><DialogTitle>{performer ? `${performer.name} · settings` : 'Performer settings'}</DialogTitle></DialogHeader>
      <DialogBody>
        {!performer && <p className="rs-pm-muted">Loading…</p>}
        {performer && <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="details">Details</TabsTrigger>
            <TabsTrigger value="photo">Photos</TabsTrigger>
            <TabsTrigger value="titles">Titles</TabsTrigger>
            <TabsTrigger value="match">Metadata Providers</TabsTrigger>
            <TabsTrigger value="merge">Duplicates &amp; merge</TabsTrigger>
          </TabsList>
          <div className="rs-pm-panel">
            <TabsContent value="match">
              <PerformerMatch performer={performer} onUpdated={changed} unmatchRemote={unmatchPerformerProvider} />
              {(performer.links || []).length > 1 && <FieldSources key={`${performer.id}:${performer.links.map(l => l.provider).join()}`} performer={performer} onChanged={changed} />}
            </TabsContent>
            <TabsContent value="photo"><PhotoTab performer={performer} onChanged={changed} /></TabsContent>
            <TabsContent value="details"><DetailsTab key={performer.id} performer={performer} onSaved={changed} onDeleted={() => { onOpenChange(false); onChanged?.(); onDeleted?.(); }} /></TabsContent>
            <TabsContent value="titles"><TitlesTab performer={performer} onChanged={changed} /></TabsContent>
            <TabsContent value="merge"><MergeTab performer={performer} onMerged={merged} /></TabsContent>
          </div>
        </Tabs>}
      </DialogBody>
    </DialogContent>
  </Dialog>;
}

/** Create a performer by hand, with an optional description. */
export function NewPerformerDialog({ open, onOpenChange, onCreated }) {
  const { toast } = useToast();
  const [name, setName] = useState('');
  const [notes, setNotes] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) { setName(''); setNotes(''); } }, [open]);

  async function submit(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const created = await createPerformer({ name: name.trim(), notes });
      onOpenChange(false);
      onCreated?.(created.id);
      toast({ title: 'Performer created', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not create performer', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent size="md">
      <DialogHeader><DialogTitle>New performer</DialogTitle></DialogHeader>
      <DialogBody>
        <form className="rs-pm-form" onSubmit={submit}>
          <FormField label="Name"><Input value={name} onChange={e => setName(e.target.value)} autoFocus /></FormField>
          <FormField label="Description"><textarea className="rs-pm-textarea" rows={4} value={notes} onChange={e => setNotes(e.target.value)} placeholder="Optional" /></FormField>
          <p className="rs-pm-muted">After creating, open Settings to link titles, add a photo, or match with a metadata provider.</p>
          <div><Button type="submit" disabled={busy || !name.trim()}>Create performer</Button></div>
        </form>
      </DialogBody>
    </DialogContent>
  </Dialog>;
}
