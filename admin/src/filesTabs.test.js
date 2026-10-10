import test from 'node:test';
import assert from 'node:assert/strict';
import { activateTab, activeTab, canGoBack, canGoForward, closeTab, goBack, goForward, initialTabs, openTab, restoreTabs, serializeTabs, syncActive, tabLabel } from './filesTabs.js';

const urls = state => state.tabs.map(tab => tab.url);

test('tabLabel uses the last folder, decoded, else the location name', () => {
  const locations = [{ id: 3, name: 'Media' }];
  assert.equal(tabLabel('/files/3/Movies/Spider%20Man', locations), 'Spider Man');
  assert.equal(tabLabel('/files/3', locations), 'Media');
  assert.equal(tabLabel('/files/9', locations), 'Storage');
  assert.equal(tabLabel('/nope', locations), 'Files');
});

test('a new tab opens next to the active one and becomes active', () => {
  let s = initialTabs('/files/1');
  s = openTab(s, '/files/1/a');
  s = openTab(activateTab(s, s.tabs[0].id), '/files/1/b');
  assert.deepEqual(urls(s), ['/files/1', '/files/1/b', '/files/1/a']);
  assert.equal(activeTab(s).url, '/files/1/b');
});

test('syncActive updates only the active tab and keeps identity when unchanged', () => {
  const s = openTab(initialTabs('/files/1'), '/files/1/a');
  assert.equal(syncActive(s, '/files/1/a'), s);
  assert.deepEqual(urls(syncActive(s, '/files/1/z')), ['/files/1', '/files/1/z']);
});

test('closing the active tab selects a neighbour and the last tab stays', () => {
  let s = openTab(openTab(initialTabs('/files/1'), '/files/1/a'), '/files/1/b');
  s = closeTab(s, s.activeId);
  assert.equal(s.tabs.length, 2);
  assert.equal(activeTab(s).url, '/files/1/a');
  s = closeTab(s, s.tabs[0].id);
  assert.equal(closeTab(s, s.tabs[0].id), s);
});

test('restoreTabs keeps saved tabs, lets the opened url win, and ignores junk', () => {
  const saved = serializeTabs(openTab(initialTabs('/files/1'), '/files/1/a'));
  const restored = restoreTabs(saved, '/files/2/x');
  assert.equal(restored.tabs.length, 2);
  assert.equal(activeTab(restored).url, '/files/2/x');
  assert.equal(restoreTabs('not json', '/files/1').tabs.length, 1);
  assert.equal(restoreTabs(JSON.stringify({ tabs: [{ id: 'a', url: '/etc' }] }), '/files/1').tabs[0].url, '/files/1');
});

test('each tab has its own back/forward history', () => {
  let s = initialTabs('/files/1');
  s = syncActive(s, '/files/1/a');
  s = syncActive(s, '/files/1/a/b');
  assert.equal(canGoBack(s), true);
  assert.equal(canGoForward(s), false);
  s = goBack(s);
  assert.equal(activeTab(s).url, '/files/1/a');
  assert.equal(canGoForward(s), true);
  s = goBack(s);
  assert.equal(activeTab(s).url, '/files/1');
  assert.equal(goBack(s), s);
  s = goForward(s);
  assert.equal(activeTab(s).url, '/files/1/a');
  // Navigating somewhere new drops the forward list.
  s = syncActive(s, '/files/1/z');
  assert.equal(canGoForward(s), false);
  // A new tab starts with a clean history.
  assert.equal(canGoBack(openTab(s, '/files/1/q')), false);
});
