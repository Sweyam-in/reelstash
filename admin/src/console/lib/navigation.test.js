import test from 'node:test';
import assert from 'node:assert/strict';
import { NAV_ENTRIES, NAV_GROUPS, SHORTCUTS, navEntryFor, tabsOf, can } from './navigation.js';

test('every g-chord letter belongs to one page', () => {
  const seen = new Map();
  for (const entry of NAV_ENTRIES.filter(e => e.shortcut)) {
    assert.ok(!seen.has(entry.shortcut), `"${entry.shortcut}" is used by ${seen.get(entry.shortcut)} and ${entry.label}`);
    seen.set(entry.shortcut, entry.label);
  }
  assert.equal(Object.keys(SHORTCUTS).length, seen.size);
});

test('paths are unique and live under /admin', () => {
  const paths = NAV_ENTRIES.map(e => e.path);
  assert.equal(new Set(paths).size, paths.length);
  assert.ok(paths.every(p => p === '/admin' || p.startsWith('/admin/')));
});

test('a detail page resolves to its list entry, and a tab to its own label', () => {
  assert.equal(navEntryFor('/admin/users/42').id, 'users');
  assert.equal(navEntryFor('/admin/storage/3').label, 'Disks');
  assert.equal(navEntryFor('/admin/scan-rules').label, 'Scan rules');
  assert.equal(navEntryFor('/admin/scan-rules').parent.id, 'storage');
  assert.equal(navEntryFor('/admin').id, 'dashboard');
  assert.equal(navEntryFor('/admin/metadata/providers').id, 'providers');
});

test('sibling tabs of one sidebar entry are found', () => {
  assert.deepEqual(tabsOf('metadata').map(t => t.label), ['People', 'Studios', 'Collections / series', 'Categories, genres & tags']);
  assert.deepEqual(tabsOf('organizer').map(t => t.path), ['/admin/organizer', '/admin/rename', '/admin/dvds']);
});

test('the sidebar stays compact: few groups, few entries', () => {
  assert.ok(NAV_GROUPS.length <= 6);
  assert.ok(NAV_GROUPS.flatMap(g => g.items).length <= 18);
  assert.ok(NAV_ENTRIES.some(e => e.id === 'audit'));
});

test('only administrators reach any entry', () => {
  assert.equal(can({ isAdmin: true }, NAV_ENTRIES[0]), true);
  assert.equal(can({ isAdmin: false }, NAV_ENTRIES[0]), false);
  assert.equal(can(null, NAV_ENTRIES[0]), false);
});
