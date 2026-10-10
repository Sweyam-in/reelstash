import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  ArrowDown, ArrowRight, ArrowUp, ArrowUpDown, Check, ChevronRight, ClipboardPaste, Copy, Download, ExternalLink,
  File, FileText, Film, Folder, FolderInput, FolderOpen, FolderPlus, Grid2X2, HardDrive, Image, Info, List, Lock,
  Pencil, Play, RefreshCw, Scissors, Search, Trash2, Upload,
} from 'lucide-react';
import {
  Button, Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle, Input, useToast,
} from '@spatika/react';
import {
  artworkUrl, createFileDir, deleteFileEntry, fileDownloadUrl, getFileNfo, saveFileNfo,
  listFiles, renameFileEntry, streamUrl, uploadFiles,
} from '../api';
import { mediaHref } from '../appLinks';
import FileContextMenu from '../components/FileContextMenu';

// Why an entry can't be changed: the downloads in-progress folder, or data a torrent is seeding.
const PROTECTED_HINT = {
  locked: 'Downloads in-progress folder: ReelStash never changes anything here',
  torrent: 'In use by qBittorrent: move or remove it from Downloads',
};

const VIDEO_RE = /\.(mp4|mkv|avi|mov|m4v|wmv|ts|flv|webm)$/i;
const IMAGE_RE = /\.(jpe?g|png|gif|webp|bmp|avif|svg)$/i;
const TEXT_RE = /\.(nfo|txt|srt|vtt|json|xml)$/i;
const DRAG_TYPE = 'application/x-reelstash-files';
const VIEW_KEY = 'reelstash-files-view';
// Items being dragged inside the page. dataTransfer can't be read during dragover, so it lives here.
let dragPayload = null;

function formatSize(bytes) {
  if (!Number.isFinite(bytes)) return '—';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes;
  let unit = -1;
  do { value /= 1024; unit += 1; } while (value >= 1024 && unit < units.length - 1);
  return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unit]}`;
}

function formatDate(value) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const time = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
  const month = date.toLocaleString(undefined, { month: 'short' });
  if (date.getFullYear() === new Date().getFullYear()) return `${month} ${date.getDate()}, ${time}`;
  return `${month} ${date.getDate()}, ${date.getFullYear()}`;
}

// Name and Type start A→Z; Modified and Size start with the newest / largest.
const SORT_COLUMNS = [['name', 'Name', 'asc'], ['modified', 'Date modified', 'desc'], ['type', 'Type', 'asc'], ['size', 'Size', 'desc']];

export const FilesChromeContext = createContext(null);
export function useFilesChrome() { return useContext(FilesChromeContext); }

// transfers: the background-paste state from useFileTransfers (FilesShell owns it so the header can show progress).
export function useFilesChromeState(transfers = null) {
  const [query, setQuery] = useState('');
  const [view, setViewState] = useState(() => { try { return localStorage.getItem(VIEW_KEY) === 'grid' ? 'grid' : 'list'; } catch { return 'list'; } });
  const [sort, setSort] = useState({ key: 'name', dir: 'asc' });
  // Cut/copy buffer, kept here so it survives switching tabs: { mode, locationId, dir, items: [{ name, isDir }] }.
  const [clipboard, setClipboard] = useState(null);
  const setView = useCallback(next => { setViewState(next); try { localStorage.setItem(VIEW_KEY, next); } catch { /* storage unavailable */ } }, []);
  const sortBy = useCallback(key => setSort(prev => prev.key === key
    ? { key, dir: prev.dir === 'asc' ? 'desc' : 'asc' }
    : { key, dir: SORT_COLUMNS.find(column => column[0] === key)?.[2] || 'asc' }), []);
  return useMemo(() => ({ query, setQuery, view, setView, sort, sortBy, clipboard, setClipboard, transfers }),
    [query, view, setView, sort, sortBy, clipboard, transfers]);
}

function toFilesUrl(locationId, path) {
  const encoded = path.split('/').filter(Boolean).map(encodeURIComponent).join('/');
  return `/files/${locationId}${encoded ? `/${encoded}` : ''}`;
}

// The address bar: crumbs you can click, or click the empty space to type a path.
export function FilesPathBar({ locationId, locationName, parts, onNavigate }) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const inputRef = useRef(null);
  const current = `/${parts.join('/')}`;

  useEffect(() => { setEditing(false); }, [locationId, current]);
  useEffect(() => { if (editing) inputRef.current?.select(); }, [editing]);

  function startEditing() { if (locationId) { setText(current === '/' ? '' : current); setEditing(true); } }
  function submit(event) {
    event.preventDefault();
    setEditing(false);
    onNavigate(toFilesUrl(locationId, text.trim()));
  }

  if (editing) return <form className="rs-fm-path is-editing" onSubmit={submit}>
    <input ref={inputRef} autoFocus aria-label="Folder path" value={text} spellCheck={false} placeholder="/folder/subfolder"
      onChange={event => setText(event.target.value)} onBlur={() => setEditing(false)} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); setEditing(false); } }} />
  </form>;

  return <nav className="rs-fm-path" aria-label="File path" onClick={event => { if (event.target === event.currentTarget) startEditing(); }}>
    <button type="button" onClick={() => onNavigate('/files')}><HardDrive size={14} /></button>
    {locationId && <>
      <ChevronRight className="rs-file-sep" size={14} />
      <button type="button" aria-current={parts.length ? undefined : 'page'} onClick={() => onNavigate(toFilesUrl(locationId, ''))}>{locationName || 'Storage'}</button>
    </>}
    {parts.map((part, index) => <React.Fragment key={`${index}-${part}`}>
      <ChevronRight className="rs-file-sep" size={14} />
      <button type="button" aria-current={index === parts.length - 1 ? 'page' : undefined} onClick={() => onNavigate(toFilesUrl(locationId, parts.slice(0, index + 1).join('/')))}>{part}</button>
    </React.Fragment>)}
    <span className="rs-fm-path-fill" onClick={startEditing} />
  </nav>;
}

export function FilesSearch() {
  const chrome = useFilesChrome();
  return <label className="rs-fm-search">
    <Search size={15} />
    <input id="rs-files-search" aria-label="Search this folder" placeholder="Search this folder" value={chrome.query} onChange={event => chrome.setQuery(event.target.value)}
      onKeyDown={event => { if (event.key === 'Escape') { chrome.setQuery(''); event.currentTarget.blur(); } }} />
  </label>;
}

function hasOsFiles(event) { return Array.from(event.dataTransfer?.types || []).includes('Files'); }

function ToolButton({ icon, label, onClick, disabled, danger, primary, title }) {
  return <button type="button" className={`rs-fm-tool ${danger ? 'is-danger' : ''} ${primary ? 'is-primary' : ''}`} disabled={disabled} onClick={onClick} title={title || label}>{icon}<span>{label}</span></button>;
}

function fileType(file) {
  if (file.isDir) return 'Folder';
  if (VIDEO_RE.test(file.name)) return 'Video';
  if (IMAGE_RE.test(file.name)) return 'Image';
  if (TEXT_RE.test(file.name)) return 'Document';
  return (file.name.split('.').pop() || 'File').toUpperCase() + ' file';
}

function FolderGlyph() {
  return <svg className="rs-folder-glyph" viewBox="0 0 72 56" aria-hidden="true">
    <path fill="#5BA3F5" d="M4 12.5C4 8.9 6.9 6 10.5 6H26l5.2 6.2H61.5C65.1 12.2 68 15.1 68 18.7V22H4V12.5z" />
    <path fill="#2F80ED" d="M4 22h64v21.5c0 3.6-2.9 6.5-6.5 6.5h-51C6.9 50 4 47.1 4 43.5V22z" />
  </svg>;
}

function FileIcon({ file, size = 20 }) {
  if (file.isDir) return <FolderGlyph />;
  const Icon = VIDEO_RE.test(file.name) ? Film : IMAGE_RE.test(file.name) ? Image : TEXT_RE.test(file.name) ? FileText : File;
  return <Icon size={size} strokeWidth={1.7} aria-hidden="true" />;
}

// Grid view shows a real preview when one is cheap to get: the file's own
// bytes for images (served inline, ignoring the download disposition), or
// generated artwork for videos already indexed into the library.
function FileThumb({ file, locationId, path }) {
  const [broken, setBroken] = useState(false);
  if (file.isDir) return <FolderGlyph />;
  const isImage = IMAGE_RE.test(file.name);
  const isVideo = VIDEO_RE.test(file.name);
  const src = isImage ? fileDownloadUrl(locationId, fullPath(path, file.name)) : isVideo && file.mediaItemId ? artworkUrl(file.mediaItemId) : null;
  if (src && !broken) return <span className={`rs-file-photo ${isVideo ? 'is-video' : ''}`}><img src={src} alt="" loading="lazy" onError={() => setBroken(true)} />{isVideo && <Play size={18} fill="white" />}</span>;
  if (isVideo) return <span className="rs-file-play"><Play size={22} fill="currentColor" /></span>;
  if (TEXT_RE.test(file.name)) return <span className="rs-file-doc"><FileText size={26} /></span>;
  return <span className="rs-file-unknown">?</span>;
}

function fullPath(path, name) { return path ? `${path}/${name}` : name; }

// A destination picker for Move: browses folders (never files) starting from
// the current directory, within the same storage location.
function MoveDialog({ open, onClose, locationId, startPath, itemNames, busy, onConfirm }) {
  const [browsePath, setBrowsePath] = useState(startPath);
  const [folders, setFolders] = useState([]);
  const [loadingFolders, setLoadingFolders] = useState(false);

  useEffect(() => { if (open) setBrowsePath(startPath); }, [open, startPath]);
  useEffect(() => {
    if (!open) return;
    let active = true;
    setLoadingFolders(true);
    listFiles(locationId, browsePath).then(result => { if (active) setFolders((result.items || []).filter(item => item.isDir)); })
      .catch(() => { if (active) setFolders([]); })
      .finally(() => { if (active) setLoadingFolders(false); });
    return () => { active = false; };
  }, [open, locationId, browsePath]);

  const parts = browsePath.split('/').filter(Boolean);

  return <Dialog open={open} onOpenChange={next => { if (!next) onClose(); }}>
    <DialogContent className="rs-file-dialog rs-move-dialog">
      <DialogHeader>
        <DialogTitle>Move {itemNames.length === 1 ? itemNames[0] : `${itemNames.length} items`}</DialogTitle>
        <DialogDescription>Choose a destination folder in the same storage location.</DialogDescription>
      </DialogHeader>
      <div className="rs-move-breadcrumbs">
        <button type="button" onClick={() => setBrowsePath('')}><HardDrive size={14} /> Root</button>
        {parts.map((part, index) => <React.Fragment key={`${index}-${part}`}><span>/</span><button type="button" onClick={() => setBrowsePath(parts.slice(0, index + 1).join('/'))}>{part}</button></React.Fragment>)}
      </div>
      <div className="rs-move-folder-list">
        {loadingFolders ? <p className="rs-move-empty">Loading…</p>
          : !folders.length ? <p className="rs-move-empty">No subfolders here.</p>
            : folders.map(folder => <button type="button" key={folder.name} className="rs-move-folder" onClick={() => setBrowsePath(fullPath(browsePath, folder.name))}><Folder size={16} /> {folder.name}<ArrowRight size={14} /></button>)}
      </div>
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
        <Button type="button" disabled={busy} onClick={() => onConfirm(browsePath)}>Move here{browsePath ? ` (/${browsePath})` : ' (Root)'}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>;
}

// A toast summarising a bulk delete/move, naming the first failure when some items could not be processed.
function bulkToast(verb, total, failures) {
  const plural = n => `${n} item${n === 1 ? '' : 's'}`;
  if (!failures.length) return { title: `${verb} ${plural(total)}`, tone: 'success' };
  return {
    title: `${verb} ${total - failures.length} of ${plural(total)}`,
    description: `${failures[0]}${failures.length > 1 ? ` (+${failures.length - 1} more)` : ''}`,
    tone: failures.length === total ? 'danger' : 'warn',
  };
}

export default function FileBrowserPage({ canAdmin = false, locations = [], onOpenInTab }) {
  const navigate = useNavigate();
  const params = useParams();
  const locationId = params.locationId;
  const path = params['*'] || '';
  const chrome = useFilesChrome();
  const query = chrome?.query || '';
  const sort = chrome?.sort || { key: 'name', dir: 'asc' };
  const view = chrome?.view || 'list';
  const clipboard = chrome?.clipboard || null;
  const fileInputRef = useRef(null);
  const collectionRef = useRef(null);
  const anchorRef = useRef('');
  const cursorRef = useRef('');
  const [files, setFiles] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [checked, setChecked] = useState(() => new Set());
  const [modal, setModal] = useState(null);
  const [renameName, setRenameName] = useState('');
  const [newFolderName, setNewFolderName] = useState('');
  const [nfo, setNfo] = useState('');
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [uploadProgress, setUploadProgress] = useState(null);
  const [revision, setRevision] = useState(0);
  const [contextMenu, setContextMenu] = useState(null);
  const [sortMenu, setSortMenu] = useState(false);
  const [dropTarget, setDropTarget] = useState('');
  const [dropActive, setDropActive] = useState(false);
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform || '');
  const mod = isMac ? '⌘' : 'Ctrl+';

  useEffect(() => {
    if (!locationId) return;
    let active = true;
    setLoading(true);
    setError('');
    listFiles(locationId, path).then(result => {
      if (active) setFiles(result.items || []);
    }).catch(err => { if (active) { setFiles([]); setError(err.message); } })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [locationId, path, revision]);

  useEffect(() => { setChecked(new Set()); setContextMenu(null); anchorRef.current = ''; cursorRef.current = ''; }, [locationId, path]);

  // A background paste ended: reload the folder, and select what landed if this is where it went.
  const finishedTransfer = chrome?.transfers?.finished;
  useEffect(() => {
    if (!finishedTransfer) return;
    const { job } = finishedTransfer;
    const p = job.payload || {};
    const landed = job.result?.landed || [];
    if (landed.length && String(p.destStorageLocationId) === String(locationId) && (p.destPath || '') === path) {
      setChecked(new Set(landed));
      anchorRef.current = cursorRef.current = landed[0];
    }
    setRevision(value => value + 1);
  }, [finishedTransfer]); // eslint-disable-line react-hooks/exhaustive-deps

  const activeLocation = locations.find(item => String(item.id) === String(locationId));
  const visibleFiles = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const factor = sort.dir === 'desc' ? -1 : 1;
    const byName = (a, b) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' });
    return files.filter(file => file.name.toLowerCase().includes(needle)).sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      let order = 0;
      if (sort.key === 'modified') order = new Date(a.modTime || 0) - new Date(b.modTime || 0);
      else if (sort.key === 'size') order = (a.size || 0) - (b.size || 0);
      else if (sort.key === 'type') order = fileType(a).localeCompare(fileType(b));
      return (order * factor) || (sort.key === 'name' ? byName(a, b) * factor : byName(a, b));
    });
  }, [files, query, sort]);

  const checkedFiles = files.filter(file => checked.has(file.name));
  const single = checkedFiles.length === 1 ? checkedFiles[0] : null;
  const selectedBytes = checkedFiles.reduce((sum, file) => sum + (file.isDir ? 0 : file.size || 0), 0);
  const allChecked = visibleFiles.length > 0 && visibleFiles.every(file => checked.has(file.name));

  function openFolder(nextPath) { navigate(toFilesUrl(locationId, nextPath)); }

  function goUp() {
    if (!path) return;
    openFolder(path.split('/').slice(0, -1).join('/'));
  }

  // A single click selects (Ctrl/⌘ toggles, Shift extends); opening is a double click or Enter.
  function selectEntry(file, event) {
    cursorRef.current = file.name;
    if (event.shiftKey && anchorRef.current) { selectRange(anchorRef.current, file.name); return; }
    anchorRef.current = file.name;
    if (event.metaKey || event.ctrlKey) toggleChecked(file.name);
    else setChecked(new Set([file.name]));
  }

  function selectRange(fromName, toName) {
    const from = visibleFiles.findIndex(file => file.name === fromName);
    const to = visibleFiles.findIndex(file => file.name === toName);
    if (from < 0 || to < 0) { setChecked(new Set([toName])); return; }
    setChecked(new Set(visibleFiles.slice(Math.min(from, to), Math.max(from, to) + 1).map(file => file.name)));
  }

  function toggleChecked(name) {
    setChecked(prev => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  }

  function toggleAll() {
    setChecked(allChecked ? new Set() : new Set(visibleFiles.map(file => file.name)));
  }

  function openEntry(file) {
    if (file.isDir) openFolder(fullPath(path, file.name));
    else if (VIDEO_RE.test(file.name) && file.mediaItemId) setModal({ kind: 'play', file });
    else if (IMAGE_RE.test(file.name)) setModal({ kind: 'preview', file });
    else if (TEXT_RE.test(file.name)) showNfo(file);
    else window.location.assign(fileDownloadUrl(locationId, fullPath(path, file.name)));
  }

  function startRename(file) {
    setRenameName(file.name);
    setModal({ kind: 'rename', file });
  }

  function refresh() { setRevision(value => value + 1); }

  async function runBulk(items, fn) {
    const failures = [];
    for (const item of items) {
      try { await fn(item); } catch (err) { failures.push(`${item.name}: ${err.message}`); }
    }
    return failures;
  }

  async function showNfo(file) {
    setNfo('Loading NFO…');
    setModal({ kind: 'nfo', file });
    try {
      const result = await getFileNfo(locationId, fullPath(path, file.name));
      setNfo(result.content || '');
    } catch (err) { setNfo(''); toast({ title: 'Could not load NFO', description: err.message, tone: 'danger' }); }
  }

  async function submitNfo(event) {
    event.preventDefault();
    if (!modal?.file) return;
    setBusy(true);
    try {
      await saveFileNfo(locationId, fullPath(path, modal.file.name), nfo);
      setModal(null);
      toast({ title: 'NFO saved', tone: 'success' });
    } catch (err) { toast({ title: 'Could not save NFO', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  async function submitRename(event) {
    event.preventDefault();
    if (!modal?.file || !renameName.trim()) return;
    setBusy(true);
    try {
      await renameFileEntry(locationId, fullPath(path, modal.file.name), renameName.trim());
      setChecked(new Set([renameName.trim()]));
      toast({ title: `Renamed to ${renameName.trim()}`, tone: 'success' });
      setModal(null);
      refresh();
    } catch (err) { toast({ title: 'Rename failed', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  async function submitDeleteSelection() {
    const items = checkedFiles;
    setBusy(true);
    const failures = await runBulk(items, file => deleteFileEntry(locationId, fullPath(path, file.name), file.isDir));
    setBusy(false);
    setModal(null);
    setChecked(new Set());
    refresh();
    toast(bulkToast('Deleted', items.length, failures));
  }

  async function submitMove(destPath) {
    setBusy(true);
    const started = await startTransfer({ mode: 'move', storageLocationId: locationId, dir: path, items: checkedFiles.map(file => file.name), destPath });
    setBusy(false);
    if (!started) return;
    setModal(null);
    setChecked(new Set());
  }

  // Queues a paste as a background job; the header shows its progress and the folder refreshes when it ends.
  async function startTransfer(request) {
    try {
      if (!chrome?.transfers) throw new Error('Transfers are not available here.');
      await chrome.transfers.start(request);
      return true;
    } catch (err) {
      toast({ title: request.mode === 'move' ? 'Can’t move here' : 'Can’t copy here', description: err.message, tone: 'danger' });
      return false;
    }
  }

  async function submitNewFolder(event) {
    event.preventDefault();
    if (!newFolderName.trim()) return;
    setBusy(true);
    try {
      await createFileDir(locationId, path, newFolderName.trim());
      toast({ title: `Created folder “${newFolderName.trim()}”`, tone: 'success' });
      setModal(null);
      setNewFolderName('');
      refresh();
    } catch (err) { toast({ title: 'Could not create folder', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  async function upload(list, destDir = path) {
    if (!canAdmin || !list || !list.length) return;
    setUploadProgress(0);
    try {
      const result = await uploadFiles(locationId, destDir, list, setUploadProgress);
      const skipped = result.skipped?.length || 0;
      toast({
        title: `Uploaded ${result.uploaded.length} file${result.uploaded.length === 1 ? '' : 's'}`,
        description: skipped ? `${skipped} skipped — name already exists.` : undefined,
        tone: skipped ? 'warn' : 'success',
      });
      refresh();
    } catch (err) { toast({ title: 'Upload failed', description: err.message, tone: 'danger' }); }
    finally { setUploadProgress(null); }
  }

  async function handleUpload(event) {
    const list = event.target.files;
    await upload(list);
    event.target.value = '';
  }

  function setClip(mode) {
    if (!canAdmin || !checkedFiles.length) return;
    chrome.setClipboard({ mode, locationId, dir: path, items: checkedFiles.map(file => ({ name: file.name, isDir: file.isDir })) });
  }

  // Copy duplicates the items into destDir; cut moves them. Used by paste and by drag-and-drop.
  // The clipboard can come from another storage location (another disk); the job copies across devices.
  async function transfer(clip, destDir) {
    if (!canAdmin || !clip) return;
    const sameLocation = String(clip.locationId) === String(locationId);
    const cut = clip.mode === 'cut';
    const items = cut && sameLocation ? clip.items.filter(item => clip.dir !== destDir && fullPath(clip.dir, item.name) !== destDir) : clip.items;
    if (!items.length) { if (cut) chrome.setClipboard(null); return; }
    const started = await startTransfer({
      mode: cut ? 'move' : 'copy', storageLocationId: clip.locationId, destStorageLocationId: locationId,
      dir: clip.dir, items: items.map(item => item.name), destPath: destDir,
    });
    if (started && cut && clipboard === clip) chrome.setClipboard(null);
  }

  const paste = (destDir = path) => transfer(clipboard, destDir);

  function gridColumns() {
    const entries = collectionRef.current?.querySelectorAll('.rs-fm-entry');
    if (!entries?.length) return 1;
    const top = entries[0].offsetTop;
    let count = 0;
    while (count < entries.length && entries[count].offsetTop === top) count += 1;
    return Math.max(1, count);
  }

  function moveCursor(key, extend) {
    if (!visibleFiles.length) return;
    const last = visibleFiles.length - 1;
    const columns = view === 'grid' ? gridColumns() : 1;
    const at = visibleFiles.findIndex(file => file.name === cursorRef.current);
    let next;
    if (at < 0) next = key === 'End' ? last : 0;
    else if (key === 'ArrowDown') next = Math.min(last, at + columns);
    else if (key === 'ArrowUp') next = Math.max(0, at - columns);
    else if (key === 'ArrowRight') next = view === 'grid' ? Math.min(last, at + 1) : at;
    else if (key === 'ArrowLeft') next = view === 'grid' ? Math.max(0, at - 1) : at;
    else next = key === 'End' ? last : 0;
    const name = visibleFiles[next].name;
    cursorRef.current = name;
    if (extend && anchorRef.current) selectRange(anchorRef.current, name);
    else { anchorRef.current = name; setChecked(new Set([name])); }
    requestAnimationFrame(() => collectionRef.current?.querySelectorAll('.rs-fm-entry')[next]?.focus());
  }

  function onKeyDown(event) {
    if (modal || contextMenu || event.defaultPrevented) return;
    if (event.target.closest?.('input:not([type="checkbox"]), textarea, select, [contenteditable="true"], [role="dialog"]')) return;
    const withMod = event.metaKey || event.ctrlKey;
    const key = event.key;
    const cursorFile = visibleFiles.find(file => file.name === cursorRef.current);
    const act = fn => { event.preventDefault(); fn(); };

    if (event.altKey && key === 'ArrowUp') return act(goUp);
    if (event.altKey) return;
    if (withMod && key === 'ArrowUp') return act(goUp);
    if (withMod && !event.shiftKey) {
      const lower = key.toLowerCase();
      if (lower === 'a') return act(() => setChecked(new Set(visibleFiles.map(file => file.name))));
      if (lower === 'f') return act(() => document.getElementById('rs-files-search')?.focus());
      if (lower === 'c' && canAdmin && checkedFiles.length) return act(() => setClip('copy'));
      if (lower === 'x' && canAdmin && checkedFiles.length) return act(() => setClip('cut'));
      if (lower === 'v' && canAdmin && clipboard) return act(() => paste(path));
      if (key === 'Backspace' && canAdmin && checkedFiles.length) return act(() => setModal({ kind: 'delete' }));
      return;
    }
    if (key === 'ArrowDown' || key === 'ArrowUp' || key === 'ArrowLeft' || key === 'ArrowRight' || key === 'Home' || key === 'End') return act(() => moveCursor(key, event.shiftKey));
    if (key === 'Enter') { const target = single || cursorFile; if (target) act(() => openEntry(target)); return; }
    if (key === 'F2') { if (canAdmin && single) act(() => startRename(single)); return; }
    if (key === 'Delete') { if (canAdmin && checkedFiles.length) act(() => setModal({ kind: 'delete' })); return; }
    if (key === 'Backspace') return act(goUp);
    if (key === 'Escape') { if (checkedFiles.length) act(() => setChecked(new Set())); }
  }
  // Re-bound every render so the handler always sees the current selection, folder and clipboard.
  useEffect(() => {
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  });

  useEffect(() => {
    if (!sortMenu) return undefined;
    const close = event => { if (!event.target.closest?.('.rs-fm-sortwrap')) setSortMenu(false); };
    document.addEventListener('pointerdown', close);
    return () => document.removeEventListener('pointerdown', close);
  }, [sortMenu]);

  function openContextMenu(event, file) {
    if (!event.currentTarget.contains(event.target) || event.target.closest('[role="dialog"]')) return;
    event.preventDefault();
    event.stopPropagation();
    if (file && !checked.has(file.name)) { setChecked(new Set([file.name])); anchorRef.current = cursorRef.current = file.name; }
    setContextMenu({ x: event.clientX, y: event.clientY, file });
  }

  function contextItems() {
    const file = contextMenu?.file;
    const targets = file ? (checked.has(file.name) ? checkedFiles : [file]) : [];
    const only = targets.length === 1 ? targets[0] : null;
    const items = [];
    if (only) {
      items.push({ label: 'Open', icon: only.isDir ? <FolderOpen size={15} /> : <ArrowRight size={15} />, shortcut: 'Enter', onSelect: () => openEntry(only) });
      if (only.isDir) items.push({ label: 'Open in new tab', icon: <ExternalLink size={15} />, onSelect: () => onOpenInTab?.(toFilesUrl(locationId, fullPath(path, only.name))) });
      if (!only.isDir && VIDEO_RE.test(only.name) && only.mediaItemId) items.push({ label: 'Open in library', icon: <Film size={15} />, onSelect: () => window.location.assign(mediaHref(`/library/${only.mediaItemId}`)) });
      if (!only.isDir) items.push({ label: 'Download', icon: <Download size={15} />, onSelect: () => window.location.assign(fileDownloadUrl(locationId, fullPath(path, only.name))) });
      if (!only.isDir) items.push({ label: canAdmin ? 'Edit NFO' : 'View NFO', icon: <FileText size={15} />, onSelect: () => showNfo(only) });
    }
    if (canAdmin && targets.length) {
      if (items.length) items.push('sep');
      items.push({ label: 'Cut', icon: <Scissors size={15} />, shortcut: `${mod}X`, onSelect: () => setClip('cut') });
      items.push({ label: 'Copy', icon: <Copy size={15} />, shortcut: `${mod}C`, onSelect: () => setClip('copy') });
      if (only?.isDir) items.push({ label: 'Paste into folder', icon: <ClipboardPaste size={15} />, disabled: !clipboard, onSelect: () => paste(fullPath(path, only.name)) });
      items.push('sep');
      if (only) items.push({ label: 'Rename', icon: <Pencil size={15} />, shortcut: 'F2', onSelect: () => startRename(only) });
      items.push({ label: 'Move to…', icon: <FolderInput size={15} />, onSelect: () => setModal({ kind: 'move' }) });
      items.push({ label: targets.length > 1 ? `Delete ${targets.length} items` : 'Delete', icon: <Trash2 size={15} />, shortcut: isMac ? '⌘⌫' : 'Del', danger: true, onSelect: () => setModal({ kind: 'delete' }) });
    }
    if (!file) {
      if (canAdmin) {
        items.push({ label: 'New folder', icon: <FolderPlus size={15} />, onSelect: () => setModal({ kind: 'newFolder' }) });
        items.push({ label: 'Upload files', icon: <Upload size={15} />, onSelect: () => fileInputRef.current?.click() });
        items.push({ label: 'Paste', icon: <ClipboardPaste size={15} />, shortcut: `${mod}V`, disabled: !clipboard, onSelect: () => paste(path) });
        items.push('sep');
      }
      items.push({ label: 'Select all', icon: <Check size={15} />, shortcut: `${mod}A`, onSelect: () => setChecked(new Set(visibleFiles.map(entry => entry.name))) });
      items.push({ label: 'Refresh', icon: <RefreshCw size={15} />, onSelect: refresh });
    }
    return items;
  }

  // Drag and drop: drag entries onto a folder to move them (hold Alt/Ctrl/⌘ to copy), or drop files from the desktop to upload.
  function dragStart(event, file) {
    if (!canAdmin) { event.preventDefault(); return; }
    const items = checked.has(file.name) ? checkedFiles : [file];
    if (!checked.has(file.name)) { setChecked(new Set([file.name])); anchorRef.current = cursorRef.current = file.name; }
    dragPayload = { locationId, dir: path, items: items.map(item => ({ name: item.name, isDir: item.isDir })) };
    event.dataTransfer.setData(DRAG_TYPE, '1');
    event.dataTransfer.effectAllowed = 'copyMove';
  }

  function dragEnd() { dragPayload = null; setDropTarget(''); setDropActive(false); }

  // destDir is the folder the pointer is over: a folder entry, or the current folder for the empty area.
  function dragOver(event, destDir, entryName = '') {
    const internal = dragPayload && String(dragPayload.locationId) === String(locationId);
    if (!canAdmin || !(internal || hasOsFiles(event))) return;
    if (internal && dragPayload.dir === destDir) return; // dropping where it already is
    if (internal && dragPayload.items.some(item => fullPath(dragPayload.dir, item.name) === destDir)) return;
    event.preventDefault();
    event.stopPropagation();
    event.dataTransfer.dropEffect = internal && !(event.altKey || event.ctrlKey || event.metaKey) ? 'move' : 'copy';
    setDropTarget(entryName);
    setDropActive(!entryName);
  }

  function drop(event, destDir) {
    event.preventDefault();
    event.stopPropagation();
    const payload = dragPayload;
    const copy = event.altKey || event.ctrlKey || event.metaKey;
    const osFiles = event.dataTransfer.files;
    dragEnd();
    if (payload && String(payload.locationId) === String(locationId)) transfer({ ...payload, mode: copy ? 'copy' : 'cut' }, destDir);
    else if (osFiles?.length) upload(osFiles, destDir);
  }

  const modalIsVideo = modal?.file && VIDEO_RE.test(modal.file.name);
  const cutNames = clipboard?.mode === 'cut' && String(clipboard.locationId) === String(locationId) && clipboard.dir === path ? new Set(clipboard.items.map(item => item.name)) : null;
  const sortLabel = SORT_COLUMNS.find(column => column[0] === sort.key)?.[1] || 'Name';
  const SortArrow = sort.dir === 'asc' ? ArrowUp : ArrowDown;
  const showTools = !!locationId;

  return <div className={`rs-fm ${checkedFiles.length ? 'has-selection' : ''} ${dropActive ? 'is-drop-active' : ''}`}
    onContextMenu={event => openContextMenu(event, null)}
    onDragOver={event => dragOver(event, path)} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget)) { setDropActive(false); setDropTarget(''); } }} onDrop={event => drop(event, path)}
    onClick={event => { if (event.target === event.currentTarget || event.target.classList?.contains('rs-fm-body')) setChecked(new Set()); }}>
    <input ref={fileInputRef} type="file" multiple hidden onChange={handleUpload} />

    {showTools && <div className="rs-fm-toolbar" role="toolbar" aria-label="File actions">
      {canAdmin && <>
        <ToolButton icon={<FolderPlus size={17} />} label="New folder" primary onClick={() => setModal({ kind: 'newFolder' })} />
        <ToolButton icon={<Upload size={17} />} label="Upload" primary onClick={() => fileInputRef.current?.click()} />
        <span className="rs-fm-tool-sep" aria-hidden="true" />
        <ToolButton icon={<Scissors size={17} />} label="Cut" title={`Cut (${mod}X)`} disabled={!checkedFiles.length} onClick={() => setClip('cut')} />
        <ToolButton icon={<Copy size={17} />} label="Copy" title={`Copy (${mod}C)`} disabled={!checkedFiles.length} onClick={() => setClip('copy')} />
        <ToolButton icon={<ClipboardPaste size={17} />} label="Paste" title={`Paste (${mod}V)`} disabled={!clipboard} onClick={() => paste(path)} />
        <span className="rs-fm-tool-sep" aria-hidden="true" />
        <ToolButton icon={<Pencil size={17} />} label="Rename" title="Rename (F2)" disabled={!single} onClick={() => single && startRename(single)} />
        <ToolButton icon={<FolderInput size={17} />} label="Move" disabled={!checkedFiles.length} onClick={() => setModal({ kind: 'move' })} />
        <ToolButton icon={<Trash2 size={17} />} label="Delete" danger title="Delete (Del)" disabled={!checkedFiles.length} onClick={() => setModal({ kind: 'delete' })} />
      </>}
      {!!single && <>
        {canAdmin && <span className="rs-fm-tool-sep" aria-hidden="true" />}
        <ToolButton icon={single.isDir ? <FolderOpen size={17} /> : <Play size={17} />} label="Open" onClick={() => openEntry(single)} />
        {!single.isDir && <ToolButton icon={<Download size={17} />} label="Download" onClick={() => window.location.assign(fileDownloadUrl(locationId, fullPath(path, single.name)))} />}
        {!single.isDir && <ToolButton icon={<FileText size={17} />} label="NFO" onClick={() => showNfo(single)} />}
        {!single.isDir && single.mediaItemId && <ToolButton icon={<Film size={17} />} label="Library" onClick={() => window.location.assign(mediaHref(`/library/${single.mediaItemId}`))} />}
      </>}
      <span className="rs-fm-tool-fill" />
      <div className="rs-fm-sortwrap">
        <button type="button" className="rs-fm-tool" aria-expanded={sortMenu} onClick={() => setSortMenu(open => !open)}><ArrowUpDown size={16} /><span>{sortLabel}</span></button>
        {sortMenu && <div className="rs-file-menu rs-fm-sortmenu" role="menu">
          {SORT_COLUMNS.map(([key, label]) => <button type="button" role="menuitemradio" aria-checked={sort.key === key} key={key} onClick={() => { chrome.sortBy(key); setSortMenu(false); }}>
            {sort.key === key ? <SortArrow size={15} /> : <span className="rs-file-menu-gap" />}{label}</button>)}
        </div>}
      </div>
      <div className="rs-fm-seg" role="group" aria-label="View">
        <button type="button" aria-pressed={view === 'list'} aria-label="List view" title="List view" onClick={() => chrome.setView('list')}><List size={16} /></button>
        <button type="button" aria-pressed={view === 'grid'} aria-label="Grid view" title="Grid view" onClick={() => chrome.setView('grid')}><Grid2X2 size={16} /></button>
      </div>
      <button type="button" className="rs-file-icon-btn" aria-label="Refresh" title="Refresh" onClick={refresh}><RefreshCw size={16} className={loading ? 'rs-spin' : ''} /></button>
    </div>}

    <div ref={collectionRef} className={`rs-fm-body rs-fm-${view}`} role="listbox" aria-multiselectable="true" aria-label="Files">
      {view === 'list' && !error && !loading && !!visibleFiles.length && <div className="rs-fm-head" role="presentation">
        <span className="rs-fm-check"><input type="checkbox" checked={allChecked} onChange={toggleAll} aria-label="Select all items" /></span>
        {SORT_COLUMNS.map(([key, label]) => <button type="button" key={key} className={`rs-fm-col rs-fm-col-${key}`} onClick={() => chrome.sortBy(key)}>{label}{sort.key === key && <SortArrow size={12} />}</button>)}
      </div>}
      {error ? <div className="rs-file-empty" role="alert"><Info size={28} /><h2>Could not open this folder</h2><p>{error}</p><Button variant="outline" onClick={refresh}>Try again</Button></div>
        : loading && !files.length ? <div className="rs-file-empty"><RefreshCw size={26} className="rs-spin" /><h2>Loading files</h2></div>
          : !visibleFiles.length ? <div className="rs-file-empty"><Folder size={30} /><h2>{query ? 'No matching files' : 'This folder is empty'}</h2><p>{query ? 'Try another search in this folder.' : canAdmin ? 'Drop files here, or use Upload.' : 'There are no visible files here yet.'}</p></div>
            : visibleFiles.map(file => <div key={file.name} role="option" aria-selected={checked.has(file.name)} tabIndex={-1} data-name={file.name}
              className={`rs-fm-entry ${checked.has(file.name) ? 'is-selected' : ''} ${cutNames?.has(file.name) ? 'is-cut' : ''} ${dropTarget === file.name ? 'is-drop-target' : ''}`}
              draggable={canAdmin} onDragStart={event => dragStart(event, file)} onDragEnd={dragEnd}
              onDragOver={file.isDir ? event => dragOver(event, fullPath(path, file.name), file.name) : undefined}
              onDrop={file.isDir ? event => drop(event, fullPath(path, file.name)) : undefined}
              onContextMenu={event => openContextMenu(event, file)}
              onClick={event => { if (event.target.closest('.rs-fm-check')) return; selectEntry(file, event); }}
              onFocus={() => { cursorRef.current = file.name; }}
              onDoubleClick={event => { if (!event.target.closest('.rs-fm-check')) openEntry(file); }}>
              <span className="rs-fm-check"><input type="checkbox" tabIndex={-1} checked={checked.has(file.name)} onChange={() => { cursorRef.current = anchorRef.current = file.name; toggleChecked(file.name); }} aria-label={`Select ${file.name}`} /></span>
              <span className="rs-fm-glyph">{view === 'grid' ? <FileThumb file={file} locationId={locationId} path={path} /> : <FileIcon file={file} size={18} />}</span>
              <span className="rs-fm-name" title={file.name}>{file.name}{file.protected && <span className="rs-fm-lock" title={PROTECTED_HINT[file.protected]}><Lock size={12} /></span>}</span>
              {view === 'list' && <span className="rs-fm-date">{formatDate(file.modTime) || '—'}</span>}
              {view === 'list' && <span className="rs-fm-type">{fileType(file)}</span>}
              {view === 'list' && <span className="rs-fm-size">{file.isDir ? '—' : formatSize(file.size)}</span>}
              {view === 'grid' && <span className="rs-fm-sub">{file.isDir ? formatDate(file.modTime) : formatSize(file.size)}</span>}
            </div>)}
      {dropActive && <div className="rs-fm-dropnote" aria-hidden="true"><Upload size={22} /> Drop to add to {path ? `/${path}` : activeLocation?.name || 'this folder'}</div>}
    </div>

    {showTools && <div className="rs-fm-status" role="status">
      <span>{visibleFiles.length} item{visibleFiles.length === 1 ? '' : 's'}</span>
      {!!checkedFiles.length && <span>{checkedFiles.length} selected{selectedBytes ? ` · ${formatSize(selectedBytes)}` : ''}</span>}
      <span className="rs-fm-tool-fill" />
      {clipboard && <span className="rs-fm-clip">{clipboard.mode === 'cut' ? 'Cut' : 'Copied'} {clipboard.items.length} item{clipboard.items.length === 1 ? '' : 's'} · paste with {mod}V</span>}
      {uploadProgress != null && <span className="rs-fm-progress">Uploading {Math.round(uploadProgress * 100)}%<i style={{ width: `${Math.round(uploadProgress * 100)}%` }} /></span>}
    </div>}

    <Dialog open={modal?.kind === 'play'} onOpenChange={open => { if (!open) setModal(null); }}><DialogContent size="xl" className="rs-file-dialog"><DialogHeader><DialogTitle>{modal?.file?.name}</DialogTitle><DialogDescription>Playing from your library</DialogDescription></DialogHeader>{modalIsVideo && modal.file.mediaItemId && <video controls autoPlay playsInline src={streamUrl(modal.file.mediaItemId, modal.file.mediaFileId)} />}</DialogContent></Dialog>
    <Dialog open={modal?.kind === 'preview'} onOpenChange={open => { if (!open) setModal(null); }}><DialogContent size="xl" className="rs-file-dialog"><DialogHeader><DialogTitle>{modal?.file?.name}</DialogTitle><DialogDescription>Image preview</DialogDescription></DialogHeader>{modal?.file && <img className="rs-file-preview" src={fileDownloadUrl(locationId, fullPath(path, modal.file.name))} alt={modal.file.name} />}</DialogContent></Dialog>
    <Dialog open={modal?.kind === 'nfo'} onOpenChange={open => { if (!open) setModal(null); }}><DialogContent size="lg" className="rs-file-dialog"><DialogHeader><DialogTitle>{canAdmin ? 'Edit NFO' : 'NFO metadata'}</DialogTitle><DialogDescription>{modal?.file?.name}</DialogDescription></DialogHeader><form onSubmit={submitNfo}><textarea className="rs-file-nfo" value={nfo} readOnly={!canAdmin} spellCheck={false} onChange={e => setNfo(e.target.value)} placeholder="No adjacent NFO file was found." />{canAdmin && <Button type="submit" disabled={busy}>Save</Button>}</form></DialogContent></Dialog>
    <Dialog open={modal?.kind === 'rename'} onOpenChange={open => { if (!open) setModal(null); }}><DialogContent className="rs-file-dialog"><form onSubmit={submitRename}><DialogHeader><DialogTitle>Rename {modal?.file?.isDir ? 'folder' : 'file'}</DialogTitle><DialogDescription>Choose a new name for {modal?.file?.name}.</DialogDescription></DialogHeader><Input autoFocus aria-label="New name" value={renameName} onFocus={event => { const dot = renameName.lastIndexOf('.'); event.target.setSelectionRange(0, dot > 0 && !modal?.file?.isDir ? dot : renameName.length); }} onChange={event => setRenameName(event.target.value)} /><DialogFooter><Button type="button" variant="ghost" onClick={() => setModal(null)}>Cancel</Button><Button type="submit" disabled={busy || !renameName.trim()}>Save name</Button></DialogFooter></form></DialogContent></Dialog>
    <Dialog open={modal?.kind === 'newFolder'} onOpenChange={open => { if (!open) { setModal(null); setNewFolderName(''); } }}><DialogContent className="rs-file-dialog"><form onSubmit={submitNewFolder}><DialogHeader><DialogTitle>New folder</DialogTitle><DialogDescription>Create a folder inside {path ? `/${path}` : (activeLocation?.name || 'this location')}.</DialogDescription></DialogHeader><Input autoFocus aria-label="Folder name" placeholder="Folder name" value={newFolderName} onChange={event => setNewFolderName(event.target.value)} /><DialogFooter><Button type="button" variant="ghost" onClick={() => setModal(null)}>Cancel</Button><Button type="submit" disabled={busy || !newFolderName.trim()}>Create folder</Button></DialogFooter></form></DialogContent></Dialog>
    <Dialog open={modal?.kind === 'delete'} onOpenChange={open => { if (!open) setModal(null); }}>
      <DialogContent className="rs-file-dialog">
        <DialogHeader>
          <DialogTitle>{checkedFiles.length === 1 ? `Delete ${checkedFiles[0].isDir ? 'folder' : 'file'}?` : `Delete ${checkedFiles.length} items?`}</DialogTitle>
          <DialogDescription>
            {checkedFiles.length === 1
              ? `${checkedFiles[0].name}${checkedFiles[0].isDir ? ' and everything inside it' : ''} will be permanently removed from this storage location. This cannot be undone.`
              : `These ${checkedFiles.length} items — and the contents of any folders among them — will be permanently removed from this storage location. This cannot be undone.`}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter><Button variant="ghost" onClick={() => setModal(null)}>Cancel</Button><Button variant="destructive" disabled={busy} onClick={submitDeleteSelection}>Delete</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    {contextMenu && <FileContextMenu x={contextMenu.x} y={contextMenu.y} items={contextItems()} onClose={() => setContextMenu(null)} />}
    <MoveDialog open={modal?.kind === 'move'} onClose={() => setModal(null)} locationId={locationId} startPath={path} itemNames={checkedFiles.map(file => file.name)} busy={busy} onConfirm={submitMove} />
  </div>;
}
