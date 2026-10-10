// Tab state for ReelStash Files: each tab remembers the URL it was showing, the active tab follows the
// router, and the set survives a reload. Pure helpers so the behaviour is testable without React.

const STORAGE_KEY = 'reelstash-files-tabs';
const FILES_URL = /^\/files\/([^/]+)(?:\/(.*))?$/;

export const isFilesUrl = url => FILES_URL.test(url || '');

function safeDecode(part) {
  try { return decodeURIComponent(part); } catch { return part; }
}

export function tabLabel(url, locations = []) {
  const match = FILES_URL.exec(url || '');
  if (!match) return 'Files';
  const parts = (match[2] || '').split('/').filter(Boolean);
  if (parts.length) return safeDecode(parts[parts.length - 1]);
  return locations.find(item => String(item.id) === match[1])?.name || 'Storage';
}

let counter = 0;
export const newTabId = () => `t${Date.now().toString(36)}${(counter++).toString(36)}`;

const makeTab = url => ({ id: newTabId(), url, back: [], forward: [] });

export function initialTabs(currentUrl) {
  const tab = makeTab(currentUrl);
  return { tabs: [tab], activeId: tab.id };
}

// Restore a saved set. The URL the page was opened with always wins for the active tab (a pasted link).
export function restoreTabs(raw, currentUrl) {
  try {
    const saved = JSON.parse(raw);
    const tabs = Array.isArray(saved?.tabs) ? saved.tabs.filter(tab => tab && typeof tab.id === 'string' && isFilesUrl(tab.url)).map(tab => ({
      id: tab.id, url: tab.url,
      back: Array.isArray(tab.back) ? tab.back.filter(isFilesUrl).slice(-50) : [],
      forward: Array.isArray(tab.forward) ? tab.forward.filter(isFilesUrl).slice(0, 50) : [],
    })) : [];
    if (!tabs.length) return initialTabs(currentUrl);
    const activeId = tabs.some(tab => tab.id === saved.activeId) ? saved.activeId : tabs[0].id;
    return syncActive({ tabs, activeId }, currentUrl);
  } catch { return initialTabs(currentUrl); }
}

export function serializeTabs(state) {
  return JSON.stringify({ tabs: state.tabs, activeId: state.activeId });
}

export function loadTabs(currentUrl) {
  let raw = null;
  try { raw = localStorage.getItem(STORAGE_KEY); } catch { /* storage unavailable */ }
  return raw ? restoreTabs(raw, currentUrl) : initialTabs(currentUrl);
}

export function saveTabs(state) {
  try { localStorage.setItem(STORAGE_KEY, serializeTabs(state)); } catch { /* storage unavailable */ }
}

export const activeTab = state => state.tabs.find(tab => tab.id === state.activeId) || state.tabs[0];

const updateActive = (state, change) => ({ ...state, tabs: state.tabs.map(tab => tab.id === state.activeId ? { ...tab, ...change } : tab) });

// Point the active tab at the URL the router is showing: a fresh navigation, so the old
// folder joins the back list and forward is discarded. Same object back when nothing changed.
export function syncActive(state, url) {
  const current = activeTab(state);
  if (!current || current.url === url) return state;
  return updateActive(state, { url, back: [...current.back, current.url].slice(-50), forward: [] });
}

export const canGoBack = state => activeTab(state).back.length > 0;
export const canGoForward = state => activeTab(state).forward.length > 0;

export function goBack(state) {
  const current = activeTab(state);
  if (!current.back.length) return state;
  return updateActive(state, { url: current.back[current.back.length - 1], back: current.back.slice(0, -1), forward: [current.url, ...current.forward] });
}

export function goForward(state) {
  const current = activeTab(state);
  if (!current.forward.length) return state;
  return updateActive(state, { url: current.forward[0], back: [...current.back, current.url], forward: current.forward.slice(1) });
}

export function openTab(state, url) {
  const tab = makeTab(url);
  const index = state.tabs.findIndex(item => item.id === state.activeId);
  const tabs = [...state.tabs];
  tabs.splice(index + 1, 0, tab);
  return { tabs, activeId: tab.id };
}

export function activateTab(state, id) {
  return state.tabs.some(tab => tab.id === id) ? { ...state, activeId: id } : state;
}

// Closing the last tab is refused; closing the active one hands focus to its neighbour.
export function closeTab(state, id) {
  if (state.tabs.length <= 1) return state;
  const index = state.tabs.findIndex(tab => tab.id === id);
  if (index < 0) return state;
  const tabs = state.tabs.filter(tab => tab.id !== id);
  const activeId = state.activeId === id ? tabs[Math.min(index, tabs.length - 1)].id : state.activeId;
  return { tabs, activeId };
}
