import React, { useEffect, useRef, useState } from 'react';
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Input, useToast } from '@spatika/react';
import { FileUp, Folder, X } from 'lucide-react';
import { addTorrents } from '../../api';
import FolderPicker from '../../components/FolderPicker';
import { folderLabel } from '../../downloadsModel';

const blank = { urls: '', category: '', tags: '', stopped: false, sequential: false, firstLast: false, skipChecking: false };

/** Add magnet links, URLs or .torrent files, with a save folder, category and start options. */
export default function AddTorrentDialog({ open, onClose, categories, onAdded }) {
  const { toast } = useToast();
  const [form, setForm] = useState(blank);
  const [files, setFiles] = useState([]);
  const [folder, setFolder] = useState(null);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const fileInput = useRef(null);
  const set = patch => setForm(current => ({ ...current, ...patch }));

  useEffect(() => { if (open) { setForm(blank); setFiles([]); setFolder(null); } }, [open]);

  const category = categories.find(c => c.name === form.category);
  const canAdd = form.urls.trim() || files.length;

  async function submit(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const res = await addTorrents({ ...form, files, storageLocationId: folder?.storageLocationId, path: folder?.path || '' });
      toast({ title: res.added === 1 ? 'Torrent added' : `${res.added} torrents added`, tone: 'success' });
      onClose();
      onAdded();
    } catch (err) { toast({ title: 'Could not add', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  function onDrop(event) {
    event.preventDefault();
    const dropped = Array.from(event.dataTransfer.files || []).filter(f => f.name.endsWith('.torrent'));
    if (dropped.length) setFiles(current => current.concat(dropped));
    const text = event.dataTransfer.getData('text');
    if (text && /^(magnet:|https?:)/.test(text.trim())) set({ urls: [form.urls.trim(), text.trim()].filter(Boolean).join('\n') });
  }

  return <>
    <Dialog open={open && !picking} onOpenChange={next => { if (!next) onClose(); }}>
      <DialogContent className="rs-dl-add">
        <DialogHeader>
          <DialogTitle>Add torrents</DialogTitle>
          <DialogDescription>Paste magnet links or URLs (one per line), or choose .torrent files.</DialogDescription>
        </DialogHeader>
        <form id="rs-dl-add-form" onSubmit={submit} onDragOver={e => e.preventDefault()} onDrop={onDrop} className="rs-dl-form">
          <textarea className="rs-dl-urls" rows={4} value={form.urls} onChange={e => set({ urls: e.target.value })} placeholder="magnet:?xt=urn:btih:…" aria-label="Magnet links or URLs" />
          <div className="rs-dl-files-pick">
            <Button type="button" size="sm" variant="secondary" onClick={() => fileInput.current?.click()}><FileUp size={14} /> Choose .torrent files</Button>
            <input ref={fileInput} type="file" accept=".torrent,application/x-bittorrent" multiple hidden onChange={e => { setFiles(current => current.concat(Array.from(e.target.files || []))); e.target.value = ''; }} />
            {files.map((f, i) => <span key={`${f.name}-${i}`} className="rs-dl-chip">{f.name}<button type="button" aria-label={`Remove ${f.name}`} onClick={() => setFiles(current => current.filter((_, j) => j !== i))}><X size={12} /></button></span>)}
          </div>
          <label className="rs-dl-label">Category
            <select value={form.category} onChange={e => set({ category: e.target.value })}>
              <option value="">None</option>
              {categories.map(c => <option key={c.name} value={c.name}>{c.name}</option>)}
            </select>
          </label>
          <div className="rs-dl-label">Save to
            <div className="rs-dl-folder-row">
              <span className="rs-dl-folder"><Folder size={14} />{folder ? folderLabel(folder) : category?.folder ? `${folderLabel(category.folder)} (category folder)` : 'qBittorrent’s default folder'}</span>
              <Button type="button" size="sm" variant="ghost" onClick={() => setPicking(true)}>Change…</Button>
              {folder && <Button type="button" size="sm" variant="ghost" onClick={() => setFolder(null)}>Default</Button>}
            </div>
          </div>
          <label className="rs-dl-label">Tags <Input value={form.tags} onChange={e => set({ tags: e.target.value })} placeholder="Comma-separated" /></label>
          <div className="rs-dl-toggles">
            <label className="rs-dl-check"><input type="checkbox" checked={form.stopped} onChange={e => set({ stopped: e.target.checked })} /> Add stopped</label>
            <label className="rs-dl-check"><input type="checkbox" checked={form.sequential} onChange={e => set({ sequential: e.target.checked })} /> Download in order</label>
            <label className="rs-dl-check"><input type="checkbox" checked={form.firstLast} onChange={e => set({ firstLast: e.target.checked })} /> First and last pieces first</label>
            <label className="rs-dl-check"><input type="checkbox" checked={form.skipChecking} onChange={e => set({ skipChecking: e.target.checked })} /> Skip hash check</label>
          </div>
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" form="rs-dl-add-form" disabled={busy || !canAdd}>Add</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    <FolderPicker open={picking} title="Save downloads to" description="While downloading, files stay in the in-progress folder; they land here when finished."
      initial={folder || category?.folder} onPick={loc => { setFolder(loc); setPicking(false); }} onClose={() => setPicking(false)} />
  </>;
}
