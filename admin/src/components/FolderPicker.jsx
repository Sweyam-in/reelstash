import React, { useEffect, useState } from 'react';
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@spatika/react';
import { ChevronRight, Folder, FolderPlus, HardDrive, Lock, RefreshCw } from 'lucide-react';
import { createFileDir, getFileLocations, listFiles } from '../api';

/**
 * Picks a folder on a storage location: { storageLocationId, storageLocation, path }.
 * Folders inside the downloads in-progress folder show a lock and can't be picked unless allowLocked.
 */
export default function FolderPicker({ open, title = 'Choose a folder', description, initial, confirmLabel = 'Use this folder', allowLocked = false, onPick, onClose }) {
  const [locations, setLocations] = useState([]);
  const [loc, setLoc] = useState(null);
  const [path, setPath] = useState('');
  const [entries, setEntries] = useState([]);
  const [here, setHere] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [naming, setNaming] = useState(false);
  const [newName, setNewName] = useState('');

  useEffect(() => {
    if (!open) return undefined;
    let live = true;
    getFileLocations().then(res => {
      if (!live) return;
      const items = res.items || [];
      setLocations(items);
      const start = items.find(l => l.id === initial?.storageLocationId) || (items.length === 1 ? items[0] : null);
      setLoc(start || null);
      setPath(start && start.id === initial?.storageLocationId ? initial?.path || '' : '');
    }).catch(err => live && setError(err.message));
    return () => { live = false; };
  }, [open, initial?.storageLocationId, initial?.path]);

  useEffect(() => {
    if (!open || !loc) { setEntries([]); return undefined; }
    let live = true;
    setLoading(true);
    setError('');
    listFiles(loc.id, path).then(res => {
      if (!live) return;
      setEntries((res.items || []).filter(item => item.isDir));
      setHere(res.protected || '');
    }).catch(err => {
      if (!live) return;
      setEntries([]);
      setError(err.message);
    }).finally(() => live && setLoading(false));
    return () => { live = false; };
  }, [open, loc, path]);

  const parts = path.split('/').filter(Boolean);
  const go = next => { setPath(next); setNaming(false); };
  const locked = here === 'locked';

  async function makeFolder(event) {
    event.preventDefault();
    const name = newName.trim();
    if (!name) return;
    try {
      const res = await createFileDir(loc.id, path, name);
      setNewName('');
      setNaming(false);
      go(res.path);
    } catch (err) { setError(err.message); }
  }

  return <Dialog open={open} onOpenChange={next => { if (!next) onClose(); }}>
    <DialogContent className="rs-dl-picker">
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>
      {!loc
        ? <div className="rs-move-folder-list">
          {locations.map(l => <button type="button" key={l.id} className="rs-move-folder" onClick={() => { setLoc(l); go(''); }}><HardDrive size={16} />{l.name}<ChevronRight size={14} /></button>)}
          {!locations.length && <p className="rs-move-empty">{error || 'No storage locations yet. Add one under Admin → Storage.'}</p>}
        </div>
        : <>
          <nav className="rs-move-breadcrumbs" aria-label="Folder path">
            {locations.length > 1 && <><button type="button" onClick={() => setLoc(null)}><HardDrive size={13} /> Disks</button><ChevronRight size={12} /></>}
            <button type="button" onClick={() => go('')}>{loc.name}</button>
            {parts.map((part, i) => <React.Fragment key={i}><ChevronRight size={12} /><button type="button" onClick={() => go(parts.slice(0, i + 1).join('/'))}>{part}</button></React.Fragment>)}
          </nav>
          <div className="rs-move-folder-list" aria-busy={loading}>
            {loading && !entries.length && <p className="rs-move-empty"><RefreshCw size={14} className="rs-spin" /></p>}
            {entries.map(e => <button type="button" key={e.name} className="rs-move-folder" onClick={() => go(parts.concat(e.name).join('/'))}>
              {e.protected === 'locked' ? <Lock size={16} /> : <Folder size={16} />}{e.name}<ChevronRight size={14} />
            </button>)}
            {!loading && !entries.length && <p className="rs-move-empty">{error || 'No folders here.'}</p>}
          </div>
          {naming
            ? <form className="rs-dl-newfolder" onSubmit={makeFolder}>
              <input autoFocus value={newName} onChange={e => setNewName(e.target.value)} placeholder="New folder name" aria-label="New folder name" />
              <Button type="submit" size="sm" disabled={!newName.trim()}>Create</Button>
              <Button type="button" size="sm" variant="ghost" onClick={() => setNaming(false)}>Cancel</Button>
            </form>
            : <Button type="button" size="sm" variant="ghost" disabled={locked} onClick={() => setNaming(true)}><FolderPlus size={14} /> New folder</Button>}
          {locked && !allowLocked && <p className="rs-dl-hint"><Lock size={13} /> This is inside the in-progress folder, which ReelStash keeps for unfinished downloads.</p>}
        </>}
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
        <Button type="button" disabled={!loc || (locked && !allowLocked)} onClick={() => onPick({ storageLocationId: loc.id, storageLocation: loc.name, path })}>{confirmLabel}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}
