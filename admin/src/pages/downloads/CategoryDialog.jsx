import React, { useEffect, useState } from 'react';
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Input, useToast } from '@spatika/react';
import { Folder } from 'lucide-react';
import { createTorrentCategory, deleteTorrentCategory, editTorrentCategory } from '../../api';
import FolderPicker from '../../components/FolderPicker';
import { folderLabel } from '../../downloadsModel';

/** Add a category, or (with `category`) change its folder or delete it. */
export default function CategoryDialog({ open, category, onClose, onSaved }) {
  const { toast } = useToast();
  const [name, setName] = useState('');
  const [folder, setFolder] = useState(null);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) { setName(category?.name || ''); setFolder(category?.folder || null); } }, [open, category]);

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    const target = folder ? { storageLocationId: folder.storageLocationId, path: folder.path } : null;
    try {
      if (category) await editTorrentCategory(category.name, target);
      else await createTorrentCategory(name.trim(), target);
      toast({ title: category ? 'Category saved' : 'Category added', tone: 'success' });
      onClose();
      onSaved();
    } catch (err) { toast({ title: 'Could not save the category', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function remove() {
    setBusy(true);
    try { await deleteTorrentCategory(category.name); toast({ title: 'Category deleted', tone: 'success' }); onClose(); onSaved(); }
    catch (err) { toast({ title: 'Could not delete the category', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  return <>
    <Dialog open={open && !picking} onOpenChange={next => { if (!next) onClose(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{category ? `Category “${category.name}”` : 'Add a category'}</DialogTitle>
          <DialogDescription>Torrents in a category save to its folder. Move rules can match on it too.</DialogDescription>
        </DialogHeader>
        <form id="rs-dl-cat-form" className="rs-dl-form" onSubmit={save}>
          {!category && <label className="rs-dl-label">Name <Input autoFocus value={name} onChange={e => setName(e.target.value)} placeholder="movies" /></label>}
          <div className="rs-dl-label">Save folder
            <div className="rs-dl-folder-row">
              <span className="rs-dl-folder"><Folder size={14} />{folder ? folderLabel(folder) : 'qBittorrent’s default folder'}</span>
              <Button type="button" size="sm" variant="ghost" onClick={() => setPicking(true)}>Change…</Button>
              {folder && <Button type="button" size="sm" variant="ghost" onClick={() => setFolder(null)}>Default</Button>}
            </div>
          </div>
        </form>
        <DialogFooter>
          {category && <Button type="button" variant="destructive-soft" disabled={busy} onClick={remove}>Delete</Button>}
          <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" form="rs-dl-cat-form" disabled={busy || (!category && !name.trim())}>Save</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    <FolderPicker open={picking} title="Category folder" initial={folder} onPick={loc => { setFolder(loc); setPicking(false); }} onClose={() => setPicking(false)} />
  </>;
}
