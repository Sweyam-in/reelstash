import {
  Activity, BellRing, BrushCleaning, CalendarClock, ChartColumn, ClipboardCheck, Copy, Disc, Film, HardDrive, House,
  LibraryBig, ListChecks, Palette, Plug, Puzzle, ScrollText, Tags, UserCog, WandSparkles,
} from 'lucide-react';

// One navigation model for the sidebar, the phone tab bar, the ⌘K palette, breadcrumbs, page titles
// and the "g then letter" shortcuts. Groups follow what an administrator is trying to do.
// An item with `tabs` is one sidebar entry whose page switches between sibling views; every tab is
// still a route of its own and still a palette entry.
//
// The backend has a single admin gate (requireAdmin: role ADMIN on every /api/admin route), so
// every entry needs an admin. `requires` is kept per entry so a finer role can slot in later.

export const NAV_GROUPS = [
  {
    id: 'overview', title: 'Overview',
    items: [
      { id: 'dashboard', label: 'Dashboard', path: '/admin', icon: House, shortcut: 'd', keywords: ['home', 'overview', 'status', 'attention'] },
      { id: 'analytics', label: 'Analytics', path: '/admin/analytics', icon: ChartColumn, shortcut: 'y', keywords: ['usage', 'watch time', 'streams', 'viewers', 'growth'] },
    ],
  },
  {
    id: 'library', title: 'Library',
    items: [
      { id: 'libraries', label: 'Libraries', path: '/admin/libraries', icon: LibraryBig, shortcut: 'l', keywords: ['folders', 'mode', 'access', 'providers'] },
      { id: 'videos', label: 'Videos', path: '/admin/videos', icon: Film, shortcut: 'v', keywords: ['titles', 'media', 'files', 'metadata', 'match'] },
      { id: 'review', label: 'Review queue', path: '/admin/review', icon: ClipboardCheck, shortcut: 'r', keywords: ['matches', 'suggestions', 'confirm', 'filename'] },
      { id: 'duplicates', label: 'Duplicates', path: '/admin/duplicates', icon: Copy, shortcut: 'x', keywords: ['identical', 'copies', 'hash', 'space'] },
      { id: 'dvds', label: 'DVDs', path: '/admin/dvds', icon: Disc, shortcut: 'c', keywords: ['dvd', 'video_ts', 'vob', 'convert', 'mp4'] },
    ],
  },
  {
    id: 'catalog', title: 'Catalog',
    items: [
      {
        id: 'metadata', label: 'Metadata', path: '/admin/metadata/performers', icon: Tags, shortcut: 'm', keywords: ['catalog'],
        tabs: [
          { id: 'performers', label: 'People', path: '/admin/metadata/performers', keywords: ['performers', 'actors', 'cast', 'sync'] },
          { id: 'studios', label: 'Studios', path: '/admin/metadata/studios', keywords: ['logos', 'sync'] },
          { id: 'series', label: 'Collections / series', path: '/admin/metadata/series', keywords: ['collections', 'shows'] },
          { id: 'categories', label: 'Categories, genres & tags', path: '/admin/metadata/categories', keywords: ['genres', 'tags'] },
        ],
      },
      { id: 'providers', label: 'Providers', path: '/admin/metadata/providers', icon: Plug, shortcut: 'p', keywords: ['tmdb', 'porndb', 'api key', 'subtitles', 'matching', 'external'] },
    ],
  },
  {
    id: 'storage', title: 'Storage',
    items: [
      {
        id: 'storage', label: 'Storage', path: '/admin/storage', icon: HardDrive, shortcut: 's', keywords: ['disks', 'mount', 'capacity', 'scan'],
        tabs: [
          { id: 'disks', label: 'Disks', path: '/admin/storage', keywords: ['mount', 'capacity', 'scan', 'locations'] },
          { id: 'scan-rules', label: 'Scan rules', path: '/admin/scan-rules', shortcut: 'e', keywords: ['exclude', 'ignore', 'skip'] },
        ],
      },
      {
        id: 'organizer', label: 'Organizer', path: '/admin/organizer', icon: WandSparkles, shortcut: 'o', keywords: ['move', 'folders', 'rules', 'cleanup'],
        tabs: [
          { id: 'rules', label: 'Folder rules', path: '/admin/organizer', keywords: ['move', 'studio', 'performer', 'cleanup'] },
          { id: 'rename', label: 'Rename pipeline', path: '/admin/rename', shortcut: 'f', keywords: ['filenames', 'template', 'apply'] },
        ],
      },
    ],
  },
  {
    id: 'operations', title: 'Operations',
    items: [
      { id: 'jobs', label: 'Jobs', path: '/admin/jobs', icon: ListChecks, shortcut: 'j', keywords: ['background', 'tasks', 'scan', 'history', 'queue', 'failed'] },
      {
        id: 'automation', label: 'Automation', path: '/admin/automation', icon: CalendarClock, shortcut: 'a', keywords: ['schedules', 'watch', 'cron'],
        tabs: [
          { id: 'schedules', label: 'Schedules & watching', path: '/admin/automation', keywords: ['cron', 'watch', 'scan'] },
          { id: 'housekeeping', label: 'Housekeeping', path: '/admin/housekeeping', icon: BrushCleaning, shortcut: 'h', keywords: ['cleanup', 'orphans', 'cache', 'database', 'trash'] },
        ],
      },
      { id: 'system', label: 'System status', path: '/admin/system', icon: Activity, shortcut: 'i', keywords: ['health', 'database', 'tools', 'uptime', 'cache', 'streams', 'ffmpeg'] },
    ],
  },
  {
    id: 'access', title: 'Access & settings',
    items: [
      { id: 'users', label: 'Users & access', path: '/admin/users', icon: UserCog, shortcut: 'u', keywords: ['accounts', 'roles', 'password', 'sign-in', 'disable'] },
      { id: 'audit', label: 'Audit log', path: '/admin/audit', icon: ScrollText, shortcut: 't', keywords: ['history', 'changes', 'who did', 'events', 'reason'] },
      { id: 'plugins', label: 'Plugins', path: '/admin/plugins', icon: Puzzle, shortcut: 'g', keywords: ['bundles', 'auth', 'sso', 'extensions'] },
      { id: 'notifications', label: 'Notifications', path: '/admin/notifications', icon: BellRing, shortcut: 'n', keywords: ['webhooks', 'alerts', 'events'] },
      { id: 'appearance', label: 'Appearance', path: '/admin/appearance', icon: Palette, shortcut: 'b', keywords: ['theme', 'color', 'accent'] },
    ],
  },
];

// A hidden entry: reachable from the dashboard and ⌘K, but not worth a permanent sidebar slot.
export const HIDDEN_PAGES = [
  { id: 'setup', label: 'Setup guide', path: '/admin/setup', group: 'Overview', keywords: ['wizard', 'first run', 'get started', 'add disk'] },
];

// Flat list of everything navigable: each item, each tab, and hidden pages.
export const NAV_ENTRIES = [
  ...NAV_GROUPS.flatMap(group => group.items.flatMap(item => (item.tabs
    ? item.tabs.map(tab => ({ ...tab, icon: tab.icon || item.icon, parent: item, group: group.title, groupId: group.id }))
    : [{ ...item, parent: item, group: group.title, groupId: group.id }]))),
  ...HIDDEN_PAGES.map(page => ({ ...page, parent: page, icon: Puzzle, groupId: 'overview' })),
];

export function can(session, entry) {
  return !!session?.isAdmin && (!entry?.requires || entry.requires === 'admin');
}

export const visibleNavGroups = session => NAV_GROUPS.filter(() => !!session?.isAdmin);

// The most specific entry whose path matches: /admin/storage/3 belongs to Storage › Disks.
export function navEntryFor(pathname) {
  let best = null;
  for (const entry of NAV_ENTRIES) {
    const match = pathname === entry.path || (entry.path !== '/admin' && pathname.startsWith(`${entry.path}/`));
    if (match && (!best || entry.path.length > best.path.length)) best = entry;
  }
  return best;
}

// Shortcut chords for `g <letter>`. Tab shortcuts go to the tab's own path.
export const SHORTCUTS = NAV_ENTRIES.filter(entry => entry.shortcut).reduce((map, entry) => {
  if (!map[entry.shortcut]) map[entry.shortcut] = entry;
  return map;
}, {});

// The sibling views of one sidebar entry (Storage › Disks, Scan rules).
export const tabsOf = parentId => NAV_ENTRIES.filter(entry => entry.parent.id === parentId);
