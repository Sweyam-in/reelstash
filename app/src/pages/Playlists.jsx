import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowDown, ArrowUp, Check, Film, GripVertical, ListVideo, Pencil, Pin, PinOff, Play, Plus, Search, Shuffle, Sparkles, Trash2 } from 'lucide-react';
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Input, useToast } from '@spatika/react';
import { addCollectionItem, artworkUrl, createCollection, deleteCollection, getCollection, getCollections, getMediaItems, moveCollectionItem, removeCollectionItem, setCollectionPinned, updateCollection } from '../api';
import SmartRulesEditor from '../components/SmartRulesEditor';
import { describeCondition, emptyRules, fromServerRules, toServerRules, validateRules } from '../smartRules';
import { monogram } from '../components/Monogram';
import { useDebounce } from './hooks/useDebounce';
import { PAGE_SIZE, useInfiniteList, VirtualWindow } from '../virtual';
import { fetchPlaylistItems, playAll } from '../queueActions';

// Pinned playlists first, then alphabetical (matches the server's ordering).
const sortPlaylists = list => [...list].sort((a, b) => Number(b.pinned) - Number(a.pinned) || a.name.localeCompare(b.name));

// Cover built from the first few titles: 1 = full-bleed poster, 2-4 = collage, 0 = tinted placeholder.
function PlaylistCover({ covers = [], name }) {
  const [broken, setBroken] = useState(() => new Set());
  const shown = covers.filter(cover => !broken.has(cover.id)).slice(0, 4);
  const mono = monogram(name);
  if (!shown.length) return <span className="rs-pl-cover is-empty rs-mono" style={mono.style}><ListVideo size={34} strokeWidth={1.4} /></span>;
  return <span className={`rs-pl-cover has-${shown.length === 3 ? 4 : shown.length}`}>
    {shown.map(cover => <CoverImage key={cover.id} cover={cover} onBroken={() => setBroken(prev => new Set(prev).add(cover.id))} />)}
    {shown.length === 3 && <span className="rs-pl-cover-fill" />}
  </span>;
}

function CoverImage({ cover, onBroken }) {
  const [kind, setKind] = useState('poster');
  const src = artworkUrl(cover.id, kind, cover.posterImageId);
  return <img key={src} src={src} alt="" decoding="async" onError={() => { if (kind === 'poster') setKind('thumbnail'); else onBroken(); }} />;
}

export function Playlists() {
  const { toast } = useToast();
  const navigate = useNavigate();
  const [playlists, setPlaylists] = useState([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [smart, setSmart] = useState(false);
  const [rules, setRules] = useState(emptyRules);
  const [saving, setSaving] = useState(false);
  const rulesProblem = smart ? validateRules(rules) : '';

  useEffect(() => {
    getCollections().then(result => setPlaylists(sortPlaylists(result.items || []))).catch(err => toast({ title: 'Could not load playlists', description: err.message, tone: 'danger' })).finally(() => setLoading(false));
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  async function togglePin(list) {
    const next = !list.pinned;
    setPlaylists(current => sortPlaylists(current.map(item => item.id === list.id ? { ...item, pinned: next } : item)));
    try { await setCollectionPinned(list.id, next); toast({ title: next ? 'Playlist pinned' : 'Playlist unpinned', tone: 'success' }); }
    catch (err) {
      setPlaylists(current => sortPlaylists(current.map(item => item.id === list.id ? { ...item, pinned: !next } : item)));
      toast({ title: 'Could not update pin', description: err.message, tone: 'danger' });
    }
  }

  async function create(event) {
    event.preventDefault();
    if (!name.trim()) return;
    setSaving(true);
    try {
      const result = await createCollection({ name: name.trim(), description: description.trim(), ...(smart ? { kind: 'SMART', rules: toServerRules(rules) } : { kind: 'MANUAL' }) });
      setCreateOpen(false);
      toast({ title: 'Playlist created', tone: 'success' });
      navigate(`/playlists/${result.id}`);
    } catch (err) { toast({ title: 'Could not create playlist', description: err.message, tone: 'danger' }); }
    finally { setSaving(false); }
  }

    return <div className="rs-page rs-playlists-page"><header className="rs-page-header"><p className="rs-eyebrow">Collections</p><div className="rs-page-title-row"><h1>Playlists<span className="rs-count">{playlists.length}</span></h1><Button onClick={() => setCreateOpen(true)}><Plus size={16} /> New playlist</Button></div><p>Group titles however you like, then come back to them.</p></header>
    {loading ? <div className="rs-status">Loading playlists…</div> : playlists.length ? <VirtualWindow className="rs-playlist-grid" items={playlists} minColumn={240} gapX={16} gapY={16} aspect={4 / 3} renderItem={list => <div className={`rs-playlist-card${list.pinned ? ' is-pinned' : ''}`}>
      <Link className="rs-playlist-link" to={`/playlists/${list.id}`} aria-label={`${list.name}, ${list.mediaCount} ${list.mediaCount === 1 ? 'title' : 'titles'}`}>
        <PlaylistCover covers={list.covers} name={list.name} />
        <span className="rs-playlist-card-copy"><strong>{list.name}{list.kind === 'SMART' && <span className="rs-smart-badge" title="Fills itself from rules"><Sparkles size={11} /> Smart</span>}</strong><small>{list.mediaCount} {list.mediaCount === 1 ? 'title' : 'titles'}{list.description ? ` · ${list.description}` : ''}</small></span>
      </Link>
      <button type="button" className={`rs-pin${list.pinned ? ' is-on' : ''}`} aria-pressed={!!list.pinned} aria-label={list.pinned ? `Unpin ${list.name}` : `Pin ${list.name}`} title={list.pinned ? 'Unpin' : 'Pin to top'} onClick={() => togglePin(list)}>{list.pinned ? <PinOff size={15} /> : <Pin size={15} />}</button>
      {list.pinned && <span className="rs-pin-badge"><Pin size={11} /> Pinned</span>}
    </div>} /> : <div className="rs-status"><span className="rs-status-icon"><ListVideo size={26} /></span><h2>Your first playlist starts here</h2><p>Group titles into collections you can return to anytime.</p><Button onClick={() => setCreateOpen(true)}><Plus size={16} /> Create playlist</Button></div>}
    <Dialog open={createOpen} onOpenChange={setCreateOpen}><DialogContent size={smart ? 'lg' : undefined}><form className="rs-playlist-form" onSubmit={create}><DialogHeader><DialogTitle>Create playlist</DialogTitle><DialogDescription>{smart ? 'A smart playlist fills itself from rules, and stays up to date as your library changes.' : 'Give this collection a name. You can add titles next.'}</DialogDescription></DialogHeader>
      <div className="rs-playlist-kind" role="radiogroup" aria-label="Playlist type">
        <button type="button" role="radio" aria-checked={!smart} className={!smart ? 'is-selected' : ''} onClick={() => setSmart(false)}><ListVideo size={16} /><span><strong>Manual</strong><small>You pick the titles</small></span></button>
        <button type="button" role="radio" aria-checked={smart} className={smart ? 'is-selected' : ''} onClick={() => setSmart(true)}><Sparkles size={16} /><span><strong>Smart</strong><small>Filled by rules</small></span></button>
      </div>
      <label>Name<Input autoFocus value={name} onChange={event => setName(event.target.value)} maxLength={120} placeholder={smart ? 'Unwatched Marvel' : 'Weekend favorites'} /></label><label>Description <span>optional</span><Input value={description} onChange={event => setDescription(event.target.value)} maxLength={240} placeholder="A few words about this playlist" /></label>
      {smart && <><SmartRulesEditor rules={rules} onChange={setRules} />{rulesProblem && <p className="rs-smart-problem" role="alert">{rulesProblem}</p>}</>}
      <DialogFooter><Button type="button" variant="ghost" onClick={() => setCreateOpen(false)}>Cancel</Button><Button type="submit" disabled={!name.trim() || saving || Boolean(rulesProblem)}>Create playlist</Button></DialogFooter></form></DialogContent></Dialog>
  </div>;
}

export function PlaylistDetail() {
  const { toast } = useToast();
  const { id } = useParams();
  const navigate = useNavigate();
  const [playlist, setPlaylist] = useState(null);
  const [addOpen, setAddOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [busy, setBusy] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [addedIds, setAddedIds] = useState(() => new Set());
  const [selected, setSelected] = useState(() => new Map());
  const [drag, setDrag] = useState(null); // { from, over } row indexes while dragging
  const [editOpen, setEditOpen] = useState(false);
  const [editName, setEditName] = useState('');
  const [editDescription, setEditDescription] = useState('');
  const [editRules, setEditRules] = useState(emptyRules);
  const pickerRef = useRef(null);
  const debouncedQuery = useDebounce(query, 250);
  const { items, total, loading, loadingMore, error: listError, hasMore, loadMore, refresh } = useInfiniteList(async ({ limit, offset }) => {
    const result = await getCollection(id, { limit, offset });
    if (offset === 0) setPlaylist(result.collection);
    return result;
  }, id);
  const library = useInfiniteList(
    ({ limit, offset }) => (addOpen ? getMediaItems({ limit, offset, ...(debouncedQuery.trim() ? { q: debouncedQuery.trim() } : {}) }) : Promise.resolve({ items: [], total: 0 })),
    addOpen ? `open:${debouncedQuery.trim()}` : 'closed',
  );
  const available = useMemo(() => library.items.filter(item => !addedIds.has(item.id)), [library.items, addedIds]);

  useEffect(() => {
    if (!addOpen) return undefined;
    let active = true;
    (async () => {
      const ids = new Set();
      let offset = 0;
      while (active) {
        const result = await getCollection(id, { limit: 200, offset });
        const page = result.items || [];
        page.forEach(item => ids.add(item.id));
        if (page.length < 200) break;
        offset += page.length;
      }
      if (active) setAddedIds(ids);
    })().catch(err => { if (active) toast({ title: 'Could not load playlist titles', description: err.message, tone: 'danger' }); });
    return () => { active = false; };
  }, [addOpen, id]);

  useEffect(() => {
    if (addOpen && !library.loading && !library.loadingMore && !library.error && library.hasMore && available.length < PAGE_SIZE) library.loadMore();
  }, [addOpen, library.loading, library.loadingMore, library.error, library.hasMore, library.loadMore, available.length]);

  function toggle(item) {
    setSelected(current => {
      const next = new Map(current);
      if (next.has(item.id)) next.delete(item.id); else next.set(item.id, item);
      return next;
    });
  }
  function closePicker(open) {
    setAddOpen(open);
    if (!open) { setSelected(new Map()); setQuery(''); }
  }
  async function addSelected() {
    setBusy(true);
    const done = [];
    try {
      for (const item of selected.values()) {
        if (addedIds.has(item.id)) { done.push(item.id); continue; }
        await addCollectionItem(id, item.id);
        done.push(item.id);
      }
      setAddedIds(current => new Set([...current, ...done]));
      await refresh();
      closePicker(false);
      toast({ title: `Added ${done.length} ${done.length === 1 ? 'title' : 'titles'}`, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not add titles', description: err.message, tone: 'danger' });
      setAddedIds(current => new Set([...current, ...done]));
      setSelected(current => { const next = new Map(current); done.forEach(d => next.delete(d)); return next; });
      await refresh();
    }
    finally { setBusy(false); }
  }
  async function remove(item) {
    try {
      await removeCollectionItem(id, item.id);
      setAddedIds(current => { const next = new Set(current); next.delete(item.id); return next; });
      await refresh();
      toast({ title: 'Title removed from playlist', tone: 'success' });
    } catch (err) { toast({ title: 'Could not remove title', description: err.message, tone: 'danger' }); }
  }
  async function move(index, to) {
    if (to < 0 || to >= total) return;
    try { await moveCollectionItem(id, items[index].id, to); await refresh(); }
    catch (err) { toast({ title: 'Could not reorder playlist', description: err.message, tone: 'danger' }); }
  }
  function endDrag(commit) {
    const current = drag;
    setDrag(null);
    if (commit && current && current.over != null && current.over !== current.from) move(current.from, current.over);
  }
  async function playPlaylist(shuffle) {
    try {
      const list = await fetchPlaylistItems(id);
      if (!playAll(list, { shuffle, navigate })) toast({ title: 'This playlist has nothing to play yet' });
    } catch (err) { toast({ title: 'Could not start the playlist', description: err.message, tone: 'danger' }); }
  }
  const isSmart = playlist?.kind === 'SMART';
  function openEdit() {
    setEditName(playlist.name);
    setEditDescription(playlist.description || '');
    setEditRules(fromServerRules(playlist.rules));
    setEditOpen(true);
  }
  const editProblem = editOpen && isSmart ? validateRules(editRules) : '';
  async function saveEdit(event) {
    event.preventDefault();
    setBusy(true);
    try {
      await updateCollection(id, { name: editName.trim(), description: editDescription.trim(), ...(isSmart ? { rules: toServerRules(editRules) } : {}) });
      setEditOpen(false);
      await refresh();
      toast({ title: 'Playlist updated', tone: 'success' });
    } catch (err) { toast({ title: 'Could not update the playlist', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function destroy() {
    setBusy(true);
    try { await deleteCollection(id); toast({ title: 'Playlist deleted', tone: 'success' }); navigate('/playlists'); }
    catch (err) { toast({ title: 'Could not delete playlist', description: err.message, tone: 'danger' }); setBusy(false); }
  }

  useEffect(() => {
    if (listError) toast({ id: `playlist-${id}-error`, title: 'Could not load playlist', description: listError, tone: 'danger' });
  }, [listError]); // eslint-disable-line react-hooks/exhaustive-deps
  return <div className="rs-page rs-playlists-page">{loading && !playlist ? <div className="rs-status">Loading playlist…</div> : playlist && <><header className="rs-page-header"><p className="rs-eyebrow">{isSmart ? 'YOUR SMART PLAYLIST' : 'YOUR PLAYLIST'}</p><div className="rs-page-title-row"><h1>{playlist.name}<span className="rs-count">{loading && !items.length ? '…' : total}</span></h1><div className="rs-playlist-actions">{total > 0 && <><Button onClick={() => playPlaylist(false)}><Play size={16} fill="currentColor" /> Play</Button><Button variant="outline" onClick={() => playPlaylist(true)}><Shuffle size={16} /> Shuffle</Button></>}<Button variant="outline" onClick={async () => { const next = !playlist.pinned; setPlaylist(p => ({ ...p, pinned: next })); try { await setCollectionPinned(id, next); toast({ title: next ? 'Playlist pinned' : 'Playlist unpinned', tone: 'success' }); } catch (err) { setPlaylist(p => ({ ...p, pinned: !next })); toast({ title: 'Could not update pin', description: err.message, tone: 'danger' }); } }} aria-pressed={!!playlist.pinned}>{playlist.pinned ? <PinOff size={16} /> : <Pin size={16} />} {playlist.pinned ? 'Unpin' : 'Pin'}</Button>{isSmart ? <Button variant="outline" onClick={openEdit}><Pencil size={16} /> Edit rules</Button> : <><Button variant="outline" onClick={() => setAddOpen(true)}><Plus size={16} /> Add titles</Button><Button variant="ghost" size="icon-sm" aria-label="Rename playlist" onClick={openEdit}><Pencil size={17} /></Button></>}<Button variant="ghost" size="icon-sm" aria-label="Delete playlist" onClick={() => setDeleteOpen(true)}><Trash2 size={17} /></Button></div></div><p>{playlist.description || (isSmart ? 'Fills itself from rules.' : 'A collection curated by you.')}</p>{isSmart && playlist.rules?.conditions && <p className="rs-smart-summary" aria-label="Rules">{playlist.rules.conditions.map((c, i) => <span key={i} className="rs-smart-chip">{describeCondition(fromServerRules({ conditions: [c] }).conditions[0])}</span>)}<small>{playlist.rules.match === 'any' ? 'Matches any of these' : 'Matches all of these'}{playlist.rules.limit ? ` · first ${playlist.rules.limit}` : ''}</small></p>}</header>
      {items.length ? <div className="rs-playlist-items"><VirtualWindow items={items} columns={1} gapX={0} gapY={0} rowHeight={69} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <div className={`rs-playlist-item${drag?.from === index ? ' is-dragging' : ''}${drag && drag.over === index && drag.from !== index ? (drag.from < index ? ' drop-after' : ' drop-before') : ''}`} draggable={!isSmart} onDragStart={event => { event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', String(item.id)); setDrag({ from: index, over: index }); }} onDragOver={event => { if (!drag) return; event.preventDefault(); event.dataTransfer.dropEffect = 'move'; if (drag.over !== index) setDrag({ ...drag, over: index }); }} onDrop={event => { event.preventDefault(); endDrag(true); }} onDragEnd={() => endDrag(false)}>{!isSmart && <GripVertical className="rs-playlist-item-grip" size={16} aria-hidden="true" />}<span className="rs-playlist-item-number">{String(index + 1).padStart(2, '0')}</span><span className="rs-playlist-item-icon"><Film size={21} /><img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={event => { event.currentTarget.style.display = 'none'; }} /></span><div><Link to={`/library/${item.id}`}>{item.title}</Link><small>{item.studioName || 'Your library'}</small></div>{!isSmart && <><Button variant="ghost" size="icon-sm" aria-label={`Move ${item.title} up`} title="Move up" disabled={index === 0} onClick={() => move(index, index - 1)}><ArrowUp size={16} /></Button><Button variant="ghost" size="icon-sm" aria-label={`Move ${item.title} down`} title="Move down" disabled={index >= total - 1} onClick={() => move(index, index + 1)}><ArrowDown size={16} /></Button><Button variant="ghost" size="icon-sm" aria-label={`Remove ${item.title}`} onClick={() => remove(item)}><Trash2 size={16} /></Button></>}</div>} /></div> : !loading && <div className="rs-status"><span className="rs-status-icon"><ListVideo size={26} /></span><h2>No titles yet</h2>{isSmart ? <><p>Nothing in your library matches these rules right now. Titles appear here as soon as they do.</p><Button onClick={openEdit}><Pencil size={16} /> Edit rules</Button></> : <><p>Add a title from your library to start this playlist.</p><Button onClick={() => setAddOpen(true)}><Plus size={16} /> Add titles</Button></>}</div>}
      {loadingMore && <p className="rs-virtual-status">Loading more titles…</p>}</>}
    <Dialog open={addOpen} onOpenChange={closePicker}><DialogContent size="lg"><DialogHeader><DialogTitle>Add to playlist</DialogTitle><DialogDescription>Choose one or more titles from your library.</DialogDescription></DialogHeader><div className="rs-playlist-picker-search"><Search size={17} /><Input aria-label="Search titles" value={query} onChange={event => setQuery(event.target.value)} placeholder="Search titles or studios" /></div><div className="rs-playlist-picker" ref={pickerRef}>{library.loading && !available.length ? <p>Loading titles…</p> : available.length ? <VirtualWindow items={available} columns={1} gapX={0} gapY={0} rowHeight={64} scrollRef={pickerRef} hasMore={library.hasMore} onNearEnd={library.loadMore} renderItem={item => <button type="button" aria-pressed={selected.has(item.id)} className={selected.has(item.id) ? 'is-selected' : undefined} disabled={busy} onClick={() => toggle(item)}><Film size={18} /><span><strong>{item.title}</strong><small>{item.studioName || 'Your library'}</small></span>{selected.has(item.id) ? <Check size={16} /> : <Plus size={16} />}</button>} /> : <p>No more matching titles.</p>}{library.loadingMore && <p>Loading more titles…</p>}</div><DialogFooter><Button variant="ghost" onClick={() => closePicker(false)}>Cancel</Button><Button disabled={!selected.size || busy} onClick={addSelected}>{busy ? 'Adding…' : selected.size ? `Add ${selected.size} title${selected.size === 1 ? '' : 's'}` : 'Add titles'}</Button></DialogFooter></DialogContent></Dialog>
    <Dialog open={editOpen} onOpenChange={setEditOpen}><DialogContent size={isSmart ? 'lg' : undefined}><form className="rs-playlist-form" onSubmit={saveEdit}><DialogHeader><DialogTitle>{isSmart ? 'Edit smart playlist' : 'Rename playlist'}</DialogTitle><DialogDescription>{isSmart ? 'Changing the rules updates which titles appear right away.' : 'Change the name or description.'}</DialogDescription></DialogHeader>
      <label>Name<Input value={editName} onChange={event => setEditName(event.target.value)} maxLength={120} /></label><label>Description <span>optional</span><Input value={editDescription} onChange={event => setEditDescription(event.target.value)} maxLength={240} /></label>
      {isSmart && <><SmartRulesEditor rules={editRules} onChange={setEditRules} />{editProblem && <p className="rs-smart-problem" role="alert">{editProblem}</p>}</>}
      <DialogFooter><Button type="button" variant="ghost" onClick={() => setEditOpen(false)}>Cancel</Button><Button type="submit" disabled={!editName.trim() || busy || Boolean(editProblem)}>Save</Button></DialogFooter></form></DialogContent></Dialog>
    <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}><DialogContent><DialogHeader><DialogTitle>Delete playlist?</DialogTitle><DialogDescription>{playlist?.name} will be removed. The titles will stay in your library.</DialogDescription></DialogHeader><DialogFooter><Button variant="ghost" onClick={() => setDeleteOpen(false)}>Cancel</Button><Button variant="destructive" disabled={busy} onClick={destroy}>Delete playlist</Button></DialogFooter></DialogContent></Dialog>
  </div>;
}
