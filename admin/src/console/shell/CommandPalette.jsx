import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { CommandDialog, CommandEmpty, CommandFooter, CommandGroup, CommandInput, CommandItem, CommandList, CommandSeparator, Kbd } from '@spatika/react';
import { Clock, Film, FolderPlus, HardDrive, LibraryBig, Play, Plus, UserPlus, Users } from 'lucide-react';
import { getAdminVideos, getLibraries, getStorageLocations, getUsers } from '../../api';
import { NAV_ENTRIES } from '../lib/navigation';
import { useDebouncedValue } from '../lib/useDebouncedValue';
import { userLabel } from '../lib/format';

const RECENT_KEY = 'reelstash-admin-recent-commands';
const MAX_RECENT = 5;

function readRecent() {
  try {
    const parsed = JSON.parse(localStorage.getItem(RECENT_KEY) || '[]');
    return Array.isArray(parsed) ? parsed.slice(0, MAX_RECENT) : [];
  } catch { return []; }
}

function writeRecent(entry) {
  try {
    const next = [entry, ...readRecent().filter(r => r.id !== entry.id)].slice(0, MAX_RECENT);
    localStorage.setItem(RECENT_KEY, JSON.stringify(next));
  } catch { /* recents are a convenience */ }
}

// Create-something actions. Each lands on its page with ?new=1, which opens the create form.
const ACTIONS = [
  { label: 'New library', icon: LibraryBig, path: '/admin/libraries?new=1', keywords: ['create', 'add'] },
  { label: 'Add storage location', icon: HardDrive, path: '/admin/storage?new=1', keywords: ['disk', 'mount', 'add'] },
  { label: 'Add a user', icon: UserPlus, path: '/admin/users?new=1', keywords: ['account', 'create'] },
  { label: 'Add a scan rule', icon: FolderPlus, path: '/admin/scan-rules', keywords: ['exclude', 'ignore'] },
  { label: 'Run a job', icon: Play, path: '/admin/jobs', keywords: ['scan', 'match', 'start'] },
  { label: 'Find videos without metadata', icon: Film, path: '/admin/videos?status=filename', keywords: ['filename', 'match'] },
];

// Rows filter client-side except videos, which the server searches.
function rank(value, search) {
  if (value.startsWith('video:')) return 1;
  const q = search.trim().toLowerCase();
  if (!q) return 1;
  const v = value.toLowerCase();
  if (v.startsWith(q)) return 1;
  return q.split(/\s+/).every(word => v.includes(word)) ? 0.5 : 0;
}

// ⌘K: jump to a page, a library, disk, user or video, or start a common action. Videos are
// searched server-side as you type; the small lists load once, the first time it opens.
export function CommandPalette({ open, onOpenChange }) {
  const navigate = useNavigate();
  const [search, setSearch] = useState('');
  const [recent, setRecent] = useState([]);
  const [videos, setVideos] = useState([]);
  const [entities, setEntities] = useState(null);
  const [people, setPeople] = useState([]);
  const needle = useDebouncedValue(search.trim(), 200);

  useEffect(() => {
    if (!open) return;
    setSearch('');
    setRecent(readRecent());
  }, [open]);

  useEffect(() => {
    if (!open || entities) return;
    let live = true;
    Promise.allSettled([getLibraries(), getStorageLocations()]).then(([libs, disks]) => {
      if (live) setEntities({ libraries: libs.value?.items || [], disks: disks.value?.items || [] });
    });
    return () => { live = false; };
  }, [open, entities]);

  useEffect(() => {
    if (!open || needle.length < 2) { setVideos([]); return undefined; }
    let live = true;
    getAdminVideos({ q: needle, limit: 6 }).then(result => { if (live) setVideos(result.items || []); }).catch(() => {});
    return () => { live = false; };
  }, [open, needle]);

  // People are searched on the server, so the palette never loads the whole user list.
  useEffect(() => {
    if (!open || needle.length < 1) { setPeople([]); return undefined; }
    let live = true;
    getUsers({ q: needle, limit: 5 }).then(result => { if (live) setPeople(result.items || []); }).catch(() => {});
    return () => { live = false; };
  }, [open, needle]);

  const go = entry => { writeRecent(entry); onOpenChange(false); navigate(entry.path); };
  const run = path => { onOpenChange(false); navigate(path); };
  const showEntities = needle.length >= 1 && entities;
  const matches = text => showEntities && text.toLowerCase().includes(needle.toLowerCase());

  return <CommandDialog open={open} onOpenChange={onOpenChange} title="Command palette" description="Search pages, videos, libraries, disks, users and actions" className="console-command" filter={rank}>
    <CommandInput value={search} onValueChange={setSearch} placeholder="Jump to a page, find a video or a user…" />
    <CommandList>
      <CommandEmpty>No results for “{search.trim()}”.</CommandEmpty>

      {!search && recent.length > 0 && <>
        <CommandGroup heading="Recent">
          {recent.map(entry => <CommandItem key={entry.id} value={`recent ${entry.id} ${entry.label}`} onSelect={() => go(entry)}>
            <Clock size={16} aria-hidden="true" /><span className="console-truncate">{entry.label}</span>
            {entry.detail && <span className="console-command__detail">{entry.detail}</span>}
          </CommandItem>)}
        </CommandGroup>
        <CommandSeparator />
      </>}

      {videos.length > 0 && <CommandGroup heading="Videos">
        {videos.map(video => <CommandItem key={video.id} value={`video:${video.id}`} onSelect={() => go({ id: `video:${video.id}`, label: video.title, detail: video.studioName, path: `/admin/videos?q=${encodeURIComponent(video.title)}` })}>
          <Film size={16} aria-hidden="true" /><span className="console-truncate">{video.title}</span>
          {video.studioName && <span className="console-command__detail">{video.studioName}</span>}
        </CommandItem>)}
      </CommandGroup>}

      {showEntities && <>
        {people.map(u => <CommandItem key={`u${u.id}`} value={`user ${u.id} ${userLabel(u)} ${u.email || ''} ${u.username || ''}`} onSelect={() => go({ id: `user:${u.id}`, label: userLabel(u), detail: 'User', path: `/admin/users/${u.id}` })}>
          <Users size={16} aria-hidden="true" /><span className="console-truncate">{userLabel(u)}</span><span className="console-command__detail">User{u.disabled ? ' · disabled' : ''}</span>
        </CommandItem>)}
        {entities.libraries.filter(l => matches(l.name)).slice(0, 4).map(l => <CommandItem key={`l${l.id}`} value={`library ${l.id} ${l.name}`} onSelect={() => go({ id: `library:${l.id}`, label: l.name, detail: 'Library', path: `/admin/libraries?q=${encodeURIComponent(l.name)}` })}>
          <LibraryBig size={16} aria-hidden="true" /><span className="console-truncate">{l.name}</span><span className="console-command__detail">Library</span>
        </CommandItem>)}
        {entities.disks.filter(d => matches(`${d.name} ${d.mountPath}`)).slice(0, 4).map(d => <CommandItem key={`d${d.id}`} value={`disk ${d.id} ${d.name} ${d.mountPath}`} onSelect={() => go({ id: `disk:${d.id}`, label: d.name, detail: 'Disk', path: `/admin/storage/${d.id}` })}>
          <HardDrive size={16} aria-hidden="true" /><span className="console-truncate">{d.name}</span><span className="console-command__detail">Disk · {d.mountPath}</span>
        </CommandItem>)}
      </>}

      <CommandGroup heading="Actions">
        {ACTIONS.map(action => <CommandItem key={action.label} value={action.label} keywords={action.keywords} onSelect={() => run(action.path)}>
          <action.icon size={16} aria-hidden="true" />{action.label}
        </CommandItem>)}
      </CommandGroup>

      <CommandGroup heading="Go to">
        {NAV_ENTRIES.map(item => <CommandItem key={item.id + item.path} value={`go ${item.label} ${item.group}`} keywords={item.keywords} onSelect={() => go({ id: `page:${item.id}`, label: item.label, detail: item.group, path: item.path })}>
          <item.icon size={16} aria-hidden="true" />{item.label}
          <span className="console-command__detail">{item.group}{item.shortcut ? ` · G ${item.shortcut.toUpperCase()}` : ''}</span>
        </CommandItem>)}
      </CommandGroup>
    </CommandList>
    <CommandFooter className="console-command__footer">
      <span><Kbd>↑</Kbd><Kbd>↓</Kbd> navigate</span><span><Kbd>↵</Kbd> open</span><span><Kbd>esc</Kbd> close</span>
    </CommandFooter>
  </CommandDialog>;
}
