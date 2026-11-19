import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger, useToast,
} from '@spatika/react';
import {
  AlertTriangle, ArrowDown, ArrowUp, ChevronDown, Download, FolderInput, Library, Pause, Play, Plus, RefreshCw, Search,
  Settings, ShieldCheck, Tag, Trash2,
} from 'lucide-react';
import { deleteTorrents, fixDownloadFolders, moveTorrents, setTorrentCategory, torrentAction } from '../../api';
import { useDownloads } from './context';
import FolderPicker from '../../components/FolderPicker';
import { formatBytes } from '../../format';
import {
  DEFAULT_SORT, etaLabel, nextSort, formatPercent, formatRatio, formatSpeed, isComplete, moveLabel, sortTorrents, torrentState,
  torrentTags, totals, visibleTorrents,
} from '../../downloadsModel';
import TorrentDetails from './TorrentDetails';

/** Delete torrents, optionally with their data; warns when the data is in a library. */
export function DeleteTorrentsDialog({ torrents, onClose, onDone }) {
  const { toast } = useToast();
  const [withFiles, setWithFiles] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => { setWithFiles(false); }, [torrents]);
  const inLibrary = (torrents || []).filter(t => t.library);
  const libraries = [...new Set(inLibrary.map(t => t.library.name))];
  const count = torrents?.length || 0;
  async function run() {
    setBusy(true);
    try {
      await deleteTorrents(torrents.map(t => t.hash), withFiles);
      toast({ title: count === 1 ? 'Torrent removed' : `${count} torrents removed`, description: withFiles ? 'Their files were deleted too.' : 'Their files were kept.', tone: 'success' });
      onDone();
    } catch (err) { toast({ title: 'Could not remove', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  return <Dialog open={!!count} onOpenChange={next => { if (!next) onClose(); }}>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{count === 1 ? `Remove “${torrents[0].name}”?` : `Remove ${count} torrents?`}</DialogTitle>
        <DialogDescription>qBittorrent stops seeding {count === 1 ? 'it' : 'them'}. The files stay where they are unless you delete them too.</DialogDescription>
      </DialogHeader>
      <label className="rs-dl-check"><input type="checkbox" checked={withFiles} onChange={e => setWithFiles(e.target.checked)} /> Also delete the downloaded files</label>
      {withFiles && !!inLibrary.length && <p className="rs-dl-warn"><AlertTriangle size={15} /><span>
        {inLibrary.length === 1 ? `“${inLibrary[0].name}” is` : `${inLibrary.length} of these are`} in your {libraries.length === 1 ? `library “${libraries[0]}”` : `libraries ${libraries.map(l => `“${l}”`).join(', ')}`}.
        Deleting the files removes {inLibrary.length === 1 ? 'that title' : 'those titles'} from the library on its next scan, along with watch history.
      </span></p>}
      <DialogFooter>
        <Button variant="ghost" onClick={onClose}>Cancel</Button>
        <Button variant={withFiles ? 'destructive' : 'default'} disabled={busy} onClick={run}>{withFiles ? 'Remove and delete files' : 'Remove'}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}

/** Move torrents to a folder through qBittorrent (it keeps seeding); the library there is scanned. */
export function MoveTorrentsDialog({ torrents, onClose, onDone }) {
  const { toast } = useToast();
  const count = torrents?.length || 0;
  async function pick(loc) {
    try {
      await moveTorrents(torrents.map(t => t.hash), loc.storageLocationId, loc.path);
      toast({ title: count === 1 ? 'Moving the torrent' : `Moving ${count} torrents`, description: 'qBittorrent keeps seeding from the new folder. A library there is scanned once the move finishes.', tone: 'success' });
      onDone();
    } catch (err) { toast({ title: 'Could not move', description: err.message, tone: 'danger' }); }
  }
  return <FolderPicker open={!!count} title={count === 1 ? `Move “${torrents[0].name}”` : `Move ${count} torrents`}
    description="qBittorrent moves the data and keeps seeding from the new place."
    initial={torrents?.[0]?.folder} confirmLabel="Move here" onPick={pick} onClose={onClose} />;
}

function Warnings({ warnings, onFixed }) {
  const { toast } = useToast();
  const navigate = useNavigate();
  const [busy, setBusy] = useState('');
  if (!warnings?.length) return null;
  async function fix(code) {
    setBusy(code);
    try { await fixDownloadFolders(code); toast({ title: 'Fixed', tone: 'success' }); onFixed(); }
    catch (err) { toast({ title: 'Could not fix it', description: err.message, tone: 'danger' }); }
    finally { setBusy(''); }
  }
  return <div className="rs-dl-warnings">
    {warnings.map(w => <div key={w.code} className="rs-dl-warning">
      <AlertTriangle size={16} /><span>{w.message}</span>
      {w.fix === 'protect-incomplete' && <Button size="sm" variant="secondary" disabled={!!busy} onClick={() => fix(w.fix)}><ShieldCheck size={14} /> Protect it</Button>}
      {w.fix === 'apply-incomplete' && <Button size="sm" variant="secondary" disabled={!!busy} onClick={() => fix(w.fix)}>Apply to qBittorrent</Button>}
      {!w.fix && <Button size="sm" variant="secondary" onClick={() => navigate('/downloads/settings')}>Open settings</Button>}
    </div>)}
  </div>;
}

function NotConnected({ snap, error }) {
  const navigate = useNavigate();
  if (!snap) return <div className="rs-file-empty"><RefreshCw size={28} className="rs-spin" /><h2>Connecting to qBittorrent</h2>{error && <p>{error}</p>}</div>;
  return <div className="rs-file-empty">
    <Download size={32} />
    <h2>{snap.configured ? 'qBittorrent can’t be reached' : 'Connect qBittorrent'}</h2>
    <p>{snap.configured ? snap.error || `ReelStash is trying ${snap.url}.` : 'Add qBittorrent’s address and sign-in, and manage your downloads from here.'}</p>
    <Button onClick={() => navigate('/downloads/settings')}><Settings size={15} /> Settings</Button>
  </div>;
}

function Progress({ value, tone }) {
  return <span className={`rs-dl-bar is-${tone}`} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round((value || 0) * 100)}>
    <span style={{ width: `${Math.min(100, (value || 0) * 100)}%` }} />
  </span>;
}

/** A column header that sorts the list: first click, reversed, then back to newest first. */
function SortHeader({ id, label, sort, onSort, num }) {
  const active = sort.key === id;
  const Arrow = sort.dir === 'asc' ? ArrowUp : ArrowDown;
  return <button type="button" className={`rs-dl-sort${num ? ' rs-dl-num' : ''}${active ? ' is-active' : ''}`}
    aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
    title={active ? 'Click to reverse; again for newest first' : `Sort by ${label.toLowerCase()}`}
    onClick={() => onSort(nextSort(sort, id))}>
    {label}{active && <Arrow size={12} />}
  </button>;
}

function TorrentRow({ t, selected, onToggle, onOpen }) {
  const st = torrentState(t.state);
  const move = moveLabel(t.move);
  const tags = torrentTags(t);
  return <div className={`rs-dl-row${selected ? ' is-selected' : ''}`} role="row" aria-selected={selected}>
    <span className="rs-fm-check rs-dl-check-col" role="gridcell"><input type="checkbox" checked={selected} onChange={onToggle} aria-label={`Select ${t.name}`} /></span>
    <button type="button" className="rs-dl-main" onClick={onOpen} role="gridcell">
      <span className="rs-dl-name" title={t.name}>{t.name}</span>
      <span className="rs-dl-meta">
        <span>{isComplete(t) ? formatBytes(t.size) : `${formatBytes((t.size || 0) - (t.amount_left || 0))} of ${formatBytes(t.size)}`}</span>
        {t.category && <span className="rs-dl-chip"><Tag size={11} />{t.category}</span>}
        {tags.map(tag => <span key={tag} className="rs-dl-chip is-tag">{tag}</span>)}
        {t.library && <span className="rs-dl-chip is-library" title="In a ReelStash library"><Library size={11} />{t.library.name}</span>}
        {move && <span className={`rs-dl-chip is-${move.tone}`} title={t.move?.message || ''}>{move.label}</span>}
      </span>
      <Progress value={t.progress} tone={st.tone} />
    </button>
    <span className="rs-dl-status" role="gridcell"><Badge variant={st.tone}>{st.label}</Badge><small>{formatPercent(t.progress)}</small></span>
    <span className="rs-dl-num" role="gridcell" title="Download speed">{formatSpeed(t.dlspeed)}</span>
    <span className="rs-dl-num" role="gridcell" title="Upload speed">{formatSpeed(t.upspeed)}</span>
    <span className="rs-dl-num" role="gridcell" title="Time left">{etaLabel(t)}</span>
    <span className="rs-dl-num" role="gridcell" title="Ratio">{formatRatio(t.ratio)}</span>
  </div>;
}

export default function TorrentsPage({ filter, category, tag, onAdd }) {
  const { snap, error, torrents, reload, openCategory } = useDownloads();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState(() => {
    try { const saved = JSON.parse(localStorage.getItem('reelstash-downloads-sort') || 'null'); if (saved?.key) return saved; } catch { /* storage off or old value */ }
    return DEFAULT_SORT;
  });
  const [selected, setSelected] = useState(() => new Set());
  const [openHash, setOpenHash] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [moving, setMoving] = useState(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => { try { localStorage.setItem('reelstash-downloads-sort', JSON.stringify(sort)); } catch { /* storage off */ } }, [sort]);
  useEffect(() => { setSelected(new Set()); }, [filter, category, tag]);

  const shown = useMemo(() => sortTorrents(visibleTorrents(torrents, { filter, category, tag, query }), sort.key, sort.dir), [torrents, filter, category, tag, query, sort]);
  // Forget selections that left the list.
  useEffect(() => {
    setSelected(current => {
      const keep = new Set([...current].filter(h => shown.some(t => t.hash === h)));
      return keep.size === current.size ? current : keep;
    });
  }, [shown]);
  const picked = shown.filter(t => selected.has(t.hash));
  const sum = totals(picked.length ? picked : shown);
  const open = torrents.find(t => t.hash === openHash) || null;
  const activeCategory = (snap?.categories || []).find(c => c.name === category) || null;

  if (!snap?.connected) return <><Warnings warnings={snap?.warnings} onFixed={reload} /><NotConnected snap={snap} error={error} /></>;

  const toggle = hash => setSelected(current => {
    const next = new Set(current);
    if (next.has(hash)) next.delete(hash); else next.add(hash);
    return next;
  });
  const allPicked = shown.length > 0 && picked.length === shown.length;

  async function act(action, label) {
    setBusy(true);
    try { await torrentAction(picked.map(t => t.hash), action); await reload(); }
    catch (err) { toast({ title: `Could not ${label}`, description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function categorize(name) {
    setBusy(true);
    try { await setTorrentCategory(picked.map(t => t.hash), name); await reload(); }
    catch (err) { toast({ title: 'Could not change the category', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  const none = !picked.length || busy;
  return <div className={`rs-dl${picked.length ? ' has-selection' : ''}`}>
    <Warnings warnings={snap.warnings} onFixed={reload} />
    <div className="rs-fm-toolbar rs-dl-toolbar" role="toolbar" aria-label="Torrent actions">
      <label className="rs-fm-search rs-dl-search"><Search size={15} /><input value={query} onChange={e => setQuery(e.target.value)} placeholder="Filter torrents" aria-label="Filter torrents" /></label>
      <span className="rs-fm-tool-sep" />
      <button type="button" className="rs-fm-tool" disabled={none} onClick={() => act('start', 'start')}><Play size={15} /> Start</button>
      <button type="button" className="rs-fm-tool" disabled={none} onClick={() => act('stop', 'stop')}><Pause size={15} /> Stop</button>
      <button type="button" className="rs-fm-tool" disabled={none} onClick={() => act('recheck', 'recheck')}><RefreshCw size={15} /> Recheck</button>
      <button type="button" className="rs-fm-tool" disabled={none} onClick={() => setMoving(picked)}><FolderInput size={15} /> Move…</button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild><button type="button" className="rs-fm-tool" disabled={none}><Tag size={15} /> Category <ChevronDown size={13} /></button></DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuLabel>Set category</DropdownMenuLabel>
          <DropdownMenuItem onSelect={() => categorize('')}>None</DropdownMenuItem>
          {(snap.categories || []).map(c => <DropdownMenuItem key={c.name} onSelect={() => categorize(c.name)}>{c.name}</DropdownMenuItem>)}
        </DropdownMenuContent>
      </DropdownMenu>
      <button type="button" className="rs-fm-tool is-danger" disabled={none} onClick={() => setDeleting(picked)}><Trash2 size={15} /> Remove…</button>
      <span className="rs-fm-tool-fill" />
      {activeCategory && <button type="button" className="rs-fm-tool" onClick={() => openCategory(activeCategory)}><Settings size={15} /> Category folder</button>}
    </div>
    <div className="rs-dl-table" role="grid" aria-label="Torrents">
      <div className="rs-dl-head" role="row">
        <span className="rs-fm-check rs-dl-check-col"><input type="checkbox" checked={allPicked} onChange={() => setSelected(allPicked ? new Set() : new Set(shown.map(t => t.hash)))} aria-label="Select all" /></span>
        <SortHeader id="name" label="Name" sort={sort} onSort={setSort} />
        <SortHeader id="status" label="Status" sort={sort} onSort={setSort} />
        <SortHeader id="down" label="Down" sort={sort} onSort={setSort} num />
        <SortHeader id="up" label="Up" sort={sort} onSort={setSort} num />
        <SortHeader id="eta" label="ETA" sort={sort} onSort={setSort} num />
        <SortHeader id="ratio" label="Ratio" sort={sort} onSort={setSort} num />
      </div>
      {shown.map(t => <TorrentRow key={t.hash} t={t} selected={selected.has(t.hash)} onToggle={() => toggle(t.hash)} onOpen={() => setOpenHash(t.hash)} />)}
      {!shown.length && <div className="rs-file-empty">
        <Download size={30} />
        <h2>{torrents.length ? 'Nothing here' : 'No torrents yet'}</h2>
        <p>{torrents.length ? 'No torrent matches this view.' : 'Add a magnet link or a .torrent file to start a download.'}</p>
        {!torrents.length && <Button onClick={onAdd}><Plus size={15} /> Add torrent</Button>}
      </div>}
    </div>
    <div className="rs-fm-status rs-dl-statusbar">
      <span>{picked.length ? `${picked.length} of ${shown.length} selected` : `${shown.length} ${shown.length === 1 ? 'torrent' : 'torrents'}`}</span>
      <span>{formatBytes(sum.size)}</span>
      {snap.server?.free_space_on_disk > 0 && <span>{formatBytes(snap.server.free_space_on_disk)} free</span>}
      {snap.version && <span className="rs-dl-version">qBittorrent {snap.version}</span>}
    </div>
    <TorrentDetails torrent={open} onClose={() => setOpenHash(null)} onChanged={reload} onMove={t => setMoving([t])} onDelete={t => setDeleting([t])} />
    <DeleteTorrentsDialog torrents={deleting} onClose={() => setDeleting(null)} onDone={() => { setDeleting(null); setSelected(new Set()); setOpenHash(null); reload(); }} />
    <MoveTorrentsDialog torrents={moving} onClose={() => setMoving(null)} onDone={() => { setMoving(null); reload(); }} />
  </div>;
}
