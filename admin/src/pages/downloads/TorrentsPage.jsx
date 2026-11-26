import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger, useToast,
} from '@spatika/react';
import {
  AlertTriangle, ArrowDown, ArrowUp, ChevronDown, Download, FolderInput, Library, MoreHorizontal, Pause, Play, Plus, RefreshCw, Search,
  Settings, ShieldCheck, Tag, Tags, Trash2,
} from 'lucide-react';
import { deleteTorrents, fixDownloadFolders, moveTorrents, setTorrentCategory, setTorrentTags, torrentAction } from '../../api';
import { useDownloads } from './context';
import FolderPicker from '../../components/FolderPicker';
import { formatBytes } from '../../format';
import {
  DEFAULT_SORT, SORT_FIRST_DIR, etaLabel, formatAdded, formatRate, nextSort, formatPercent, formatRatio, formatSpeed, isComplete, moveLabel, sortTorrents, torrentState,
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

/** A column header that sorts the list. The first click uses that column's natural direction; the next click reverses it. */
function SortHeader({ id, label, sort, onSort, num }) {
  const active = sort.key === id;
  const Arrow = sort.dir === 'asc' ? ArrowUp : ArrowDown;
  return <button type="button" className={`rs-dl-sort${num ? ' rs-dl-num' : ''}${active ? ' is-active' : ''}`}
    aria-sort={active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'}
    title={active ? (sort.dir === 'asc' ? 'Sorted ascending. Click to reverse.' : 'Sorted descending. Click to reverse.') : `Sort by ${label.toLowerCase()}`}
    onClick={() => onSort(nextSort(sort, id))}>
    {label}{active && <Arrow size={12} />}
  </button>;
}

function storedSort() {
  try {
    const saved = JSON.parse(localStorage.getItem('reelstash-downloads-sort') || 'null');
    if (saved && Object.prototype.hasOwnProperty.call(SORT_FIRST_DIR, saved.key) && (saved.dir === 'asc' || saved.dir === 'desc')) {
      return { key: saved.key, dir: saved.dir };
    }
  } catch { /* storage off or an old value */ }
  return DEFAULT_SORT;
}

function TorrentRow({ t, selected, onToggle, onOpen }) {
  const st = torrentState(t.state);
  const move = moveLabel(t.move);
  const tags = torrentTags(t);
  const size = Number(t.size) || 0;
  const left = Number(t.amount_left) || 0;
  const seeds = Number(t.num_seeds) || 0;
  const peers = Number(t.num_leechs) || 0;
  const down = formatSpeed(t.dlspeed);
  const up = formatSpeed(t.upspeed);
  return <div className={`rs-dl-row${selected ? ' is-selected' : ''}`} role="row" aria-selected={selected}>
    <span className="rs-fm-check rs-dl-check-col" role="gridcell">
      <input type="checkbox" checked={selected} aria-label={`Select ${t.name}`}
        onPointerDown={e => { e.currentTarget.dataset.shift = e.shiftKey ? '1' : ''; }}
        onChange={e => {
          const shift = e.currentTarget.dataset.shift === '1';
          e.currentTarget.dataset.shift = '';
          onToggle(shift);
        }} />
    </span>
    <button type="button" className="rs-dl-main" onClick={onOpen} role="gridcell">
      <span className="rs-dl-name" title={t.name}>{t.name}</span>
      <span className="rs-dl-meta">
        <span>{isComplete(t) ? formatBytes(size) : `${formatBytes(size - left)} of ${formatBytes(size)}`}</span>
        {t.category && <span className="rs-dl-chip"><Tag size={11} />{t.category}</span>}
        {tags.map(tag => <span key={tag} className="rs-dl-chip is-tag">{tag}</span>)}
        {t.library && <span className="rs-dl-chip is-library" title="In a ReelStash library"><Library size={11} />{t.library.name}</span>}
        {move && <span className={`rs-dl-chip is-${move.tone}`} title={t.move?.message || ''}>{move.label}</span>}
      </span>
      <Progress value={t.progress} tone={st.tone} />
    </button>
    <span className="rs-dl-status" role="gridcell"><Badge variant={st.tone}>{st.label}</Badge><small>{formatPercent(t.progress)}</small></span>
    <span className={`rs-dl-num${down === '—' ? '' : ' is-down'}`} role="gridcell" title={down === '—' ? 'Not downloading' : `Download ${down}`}>{down}</span>
    <span className={`rs-dl-num${up === '—' ? '' : ' is-up'}`} role="gridcell" title={up === '—' ? 'Not uploading' : `Upload ${up}`}>{up}</span>
    <span className="rs-dl-num" role="gridcell" title="Size">{formatBytes(size)}</span>
    <span className="rs-dl-num" role="gridcell" title="Time left">{etaLabel(t) || '—'}</span>
    <span className="rs-dl-num" role="gridcell" title={t.added_on ? new Date(t.added_on * 1000).toLocaleString() : 'Added'}>{formatAdded(t.added_on)}</span>
    <span className="rs-dl-num" role="gridcell" title={`${seeds} seeds, ${peers} peers`}>{seeds}/{peers}</span>
    <span className="rs-dl-num" role="gridcell" title="Share ratio">{formatRatio(t.ratio)}</span>
  </div>;
}

export default function TorrentsPage({ filter, category, tag, onAdd }) {
  const { snap, error, torrents, reload, openCategory } = useDownloads();
  const { toast } = useToast();
  const [query, setQuery] = useState('');
  const [sort, setSort] = useState(storedSort);
  const [selected, setSelected] = useState(() => new Set());
  const [openHash, setOpenHash] = useState(null);
  const [deleting, setDeleting] = useState(null);
  const [moving, setMoving] = useState(null);
  const [busy, setBusy] = useState(false);
  const [tagName, setTagName] = useState('');
  const anchor = useRef(null);

  useEffect(() => { try { localStorage.setItem('reelstash-downloads-sort', JSON.stringify(sort)); } catch { /* storage off */ } }, [sort]);
  useEffect(() => { setSelected(new Set()); anchor.current = null; }, [filter, category, tag]);

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

  const toggle = (hash, shiftKey) => {
    setSelected(current => {
      const next = new Set(current);
      const ids = shown.map(t => t.hash);
      const index = ids.indexOf(hash);
      if (shiftKey && anchor.current) {
        const from = ids.indexOf(anchor.current);
        if (from >= 0 && index >= 0) {
          const [start, end] = from < index ? [from, index] : [index, from];
          const select = !current.has(hash);
          for (let i = start; i <= end; i += 1) {
            if (select) next.add(ids[i]);
            else next.delete(ids[i]);
          }
          return next;
        }
      }
      if (next.has(hash)) next.delete(hash);
      else next.add(hash);
      return next;
    });
    anchor.current = hash;
  };
  const allPicked = shown.length > 0 && picked.length === shown.length;
  const pickedTags = [...new Set(picked.flatMap(torrentTags))].sort((a, b) => a.localeCompare(b));

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
  async function applyTags(add, remove) {
    const clean = names => names.flatMap(name => String(name).split(',')).map(name => name.trim()).filter(Boolean);
    const adding = clean(add);
    const removing = clean(remove);
    if (!adding.length && !removing.length) return;
    setBusy(true);
    try {
      await setTorrentTags(picked.map(t => t.hash), { add: adding, remove: removing });
      setTagName('');
      await reload();
    } catch (err) { toast({ title: 'Could not change tags', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  const none = !picked.length || busy;
  return <div className={`rs-dl${picked.length ? ' has-selection' : ''}`}>
    <Warnings warnings={snap.warnings} onFixed={reload} />
    <div className="rs-fm-toolbar rs-dl-toolbar" role="toolbar" aria-label="Torrent actions">
      <label className="rs-fm-search rs-dl-search"><Search size={15} /><input value={query} onChange={e => setQuery(e.target.value)} placeholder="Name, tag, or hash" aria-label="Filter by name, tag, or hash" /></label>
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
      <DropdownMenu>
        <DropdownMenuTrigger asChild><button type="button" className="rs-fm-tool" disabled={none}><Tags size={15} /> Tag <ChevronDown size={13} /></button></DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <form className="rs-dl-menu-form" onSubmit={e => { e.preventDefault(); applyTags([tagName], []); }}>
            <input value={tagName} onChange={e => setTagName(e.target.value)} placeholder="New tag" aria-label="New tag"
              onKeyDown={e => e.stopPropagation()} onPointerDown={e => e.stopPropagation()} />
            <Button type="submit" size="sm" variant="secondary" disabled={!tagName.trim() || busy}>Add</Button>
          </form>
          {!!(snap.tags || []).length && <DropdownMenuLabel>Add a tag</DropdownMenuLabel>}
          {(snap.tags || []).map(name => <DropdownMenuItem key={name} onSelect={() => applyTags([name], [])}>{name}</DropdownMenuItem>)}
          {!!pickedTags.length && <>
            <DropdownMenuSeparator />
            <DropdownMenuLabel>Remove a tag</DropdownMenuLabel>
            {pickedTags.map(name => <DropdownMenuItem key={`remove-${name}`} onSelect={() => applyTags([], [name])}>{name}</DropdownMenuItem>)}
          </>}
        </DropdownMenuContent>
      </DropdownMenu>
      <DropdownMenu>
        <DropdownMenuTrigger asChild><button type="button" className="rs-fm-tool" disabled={none}><MoreHorizontal size={15} /> More</button></DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onSelect={() => act('forceStart', 'force start')}>Force start</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => act('reannounce', 'reannounce')}>Reannounce to trackers</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => act('topPrio', 'move to the top of the queue')}>Move to top of queue</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => act('bottomPrio', 'move to the bottom of the queue')}>Move to bottom of queue</DropdownMenuItem>
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
        <SortHeader id="size" label="Size" sort={sort} onSort={setSort} num />
        <SortHeader id="eta" label="ETA" sort={sort} onSort={setSort} num />
        <SortHeader id="added" label="Added" sort={sort} onSort={setSort} num />
        <SortHeader id="peers" label="Peers" sort={sort} onSort={setSort} num />
        <SortHeader id="ratio" label="Ratio" sort={sort} onSort={setSort} num />
      </div>
      {shown.map(t => <TorrentRow key={t.hash} t={t} selected={selected.has(t.hash)} onToggle={shift => toggle(t.hash, shift)} onOpen={() => setOpenHash(t.hash)} />)}
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
      <span className="rs-dl-status-speeds" aria-label={picked.length ? 'Speed of the selection' : 'Speed of this list'}>
        <span className="is-down"><ArrowDown size={13} />{formatRate(sum.down)}</span>
        <span className="is-up"><ArrowUp size={13} />{formatRate(sum.up)}</span>
      </span>
      {snap.server?.free_space_on_disk > 0 && <span>{formatBytes(snap.server.free_space_on_disk)} free</span>}
      {snap.version && <span className="rs-dl-version">qBittorrent {snap.version}</span>}
    </div>
    <TorrentDetails torrent={open} onClose={() => setOpenHash(null)} onChanged={reload} onMove={t => setMoving([t])} onDelete={t => setDeleting([t])} />
    <DeleteTorrentsDialog torrents={deleting} onClose={() => setDeleting(null)} onDone={() => { setDeleting(null); setSelected(new Set()); setOpenHash(null); reload(); }} />
    <MoveTorrentsDialog torrents={moving} onClose={() => setMoving(null)} onDone={() => { setMoving(null); reload(); }} />
  </div>;
}
