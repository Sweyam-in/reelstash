import React, { useEffect, useState } from 'react';
import { ArrowRight, ListVideo } from 'lucide-react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, Input, Button, useToast } from '../ui';
import { addCollectionItem, createCollection, getCollections } from '../api';

// "Add to playlist": pick one of the person's manual playlists or make a new one holding the title.
export default function AddToPlaylist({ mediaItemId, open, onOpenChange }) {
  const { toast } = useToast();
  const [playlists, setPlaylists] = useState([]);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) getCollections().then(result => setPlaylists((result.items || []).filter(item => item.kind === 'MANUAL'))).catch(err => toast({ title: 'Could not load playlists', description: err.message, tone: 'danger' })); }, [open, toast]);
  async function add(id, label) {
    setBusy(true);
    try { await addCollectionItem(id, mediaItemId); toast({ title: `Added to ${label}`, tone: 'success' }); onOpenChange(false); }
    catch (err) { toast({ title: 'Could not add to playlist', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function create(event) {
    event.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    try {
      const result = await createCollection({ name: name.trim(), kind: 'MANUAL' });
      await addCollectionItem(result.id, mediaItemId);
      toast({ title: `Added to ${name.trim()}`, tone: 'success' });
      setName('');
      onOpenChange(false);
    } catch (err) { toast({ title: 'Could not create playlist', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent><DialogHeader><DialogTitle>Add to playlist</DialogTitle><DialogDescription>Choose a playlist or make a new one.</DialogDescription></DialogHeader><div className="rs-playlist-picker">{playlists.map(list => <button key={list.id} type="button" disabled={busy} onClick={() => add(list.id, list.name)}><ListVideo size={18} /><span><strong>{list.name}</strong><small>{list.mediaCount} titles</small></span><ArrowRight size={16} /></button>)}{!playlists.length && <p>No playlists yet. Create one below.</p>}</div><form className="rs-add-playlist-form" onSubmit={create}><Input aria-label="New playlist name" placeholder="New playlist name" value={name} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={busy || !name.trim()}>Create & add</Button></form></DialogContent></Dialog>;
}
