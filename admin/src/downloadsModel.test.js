import test from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_SORT, etaLabel, filesHref, nextSort, filterCounts, folderLabel, formatPercent, formatRatio, formatSpan, formatSpeed,
  fromKiB, moveLabel, sortTorrents, toKiB, torrentState, totals, visibleTorrents,
} from './downloadsModel.js';

const torrents = [
  { hash: 'a', name: 'Alpha Movie', state: 'downloading', progress: 0.4, dlspeed: 2048, upspeed: 0, category: 'movies', tags: 'hd, new', added_on: 3, size: 10, eta: 125 },
  { hash: 'b', name: 'Beta Show', state: 'stalledUP', progress: 1, dlspeed: 0, upspeed: 512, category: 'tv', tags: '', added_on: 1, size: 30, ratio: 1.5 },
  { hash: 'c', name: 'Gamma', state: 'stoppedUP', progress: 1, dlspeed: 0, upspeed: 0, category: '', tags: 'hd', added_on: 2, size: 20 },
  { hash: 'd', name: 'Delta', state: 'missingFiles', progress: 0.1, dlspeed: 0, upspeed: 0, category: 'movies', tags: '', added_on: 4, size: 5 },
];

test('torrentState maps qBittorrent states to labels, tones and groups', () => {
  assert.deepEqual(torrentState('stalledUP'), { label: 'Seeding', tone: 'success', group: 'seeding' });
  assert.deepEqual(torrentState('pausedDL'), { label: 'Stopped', tone: 'neutral', group: 'stopped' });
  assert.equal(torrentState('missingFiles').group, 'errored');
  assert.equal(torrentState('whatever').label, 'Unknown');
});

test('filterCounts counts every sidebar filter', () => {
  assert.deepEqual(filterCounts(torrents), { all: 4, downloading: 1, seeding: 1, completed: 2, stopped: 1, active: 2, errored: 1 });
});

test('visibleTorrents combines filter, category, tag and search', () => {
  const names = list => list.map(t => t.hash).join('');
  assert.equal(names(visibleTorrents(torrents, { filter: 'completed' })), 'bc');
  assert.equal(names(visibleTorrents(torrents, { category: 'movies' })), 'ad');
  assert.equal(names(visibleTorrents(torrents, { category: '-' })), 'c');
  assert.equal(names(visibleTorrents(torrents, { tag: 'hd' })), 'ac');
  assert.equal(names(visibleTorrents(torrents, { query: 'show' })), 'b');
  assert.equal(names(visibleTorrents(torrents, { query: 'TV' })), 'b');
});

test('sortTorrents orders by the chosen column and direction', () => {
  const order = (...args) => sortTorrents(torrents, ...args).map(t => t.hash).join('');
  assert.equal(order(), 'dacb');
  assert.equal(order('name'), 'abdc');
  assert.equal(order('name', 'desc'), 'cdba');
  assert.equal(order('size'), 'bcad');
  assert.equal(order('down'), 'abcd');
  assert.equal(order('eta')[0], 'a'); // only the downloading torrent has a time left
});

test('nextSort cycles first direction, reversed, then newest first', () => {
  let s = DEFAULT_SORT;
  s = nextSort(s, 'name'); assert.deepEqual(s, { key: 'name', dir: 'asc' });
  s = nextSort(s, 'name'); assert.deepEqual(s, { key: 'name', dir: 'desc' });
  s = nextSort(s, 'name'); assert.deepEqual(s, DEFAULT_SORT);
  assert.deepEqual(nextSort({ key: 'name', dir: 'asc' }, 'down'), { key: 'down', dir: 'desc' });
});

test('formatting', () => {
  assert.equal(formatSpeed(0), '—');
  assert.equal(formatSpeed(2048), '2.0 KB/s');
  assert.equal(formatSpan(45), '45s');
  assert.equal(formatSpan(125), '2m');
  assert.equal(formatSpan(3 * 3600 + 300), '3h 5m');
  assert.equal(formatSpan(2 * 86400 + 4 * 3600), '2d 4h');
  assert.equal(formatSpan(8640000), '∞');
  assert.equal(etaLabel(torrents[0]), '2m');
  assert.equal(etaLabel(torrents[1]), '');
  assert.equal(formatRatio(1.234), '1.23');
  assert.equal(formatRatio(-1), '—');
  assert.equal(formatPercent(0.4567), '45.6%');
  assert.equal(toKiB(10240), 10);
  assert.equal(fromKiB('10'), 10240);
  assert.equal(fromKiB(-3), 0);
});

test('moveLabel describes what the rules did', () => {
  assert.equal(moveLabel(null), null);
  assert.deepEqual(moveLabel({ status: 'MOVED', ruleName: 'Movies' }), { label: 'Moved · Movies', tone: 'success' });
  assert.equal(moveLabel({ status: 'FAILED' }).tone, 'danger');
  assert.equal(moveLabel({ status: 'SKIPPED', message: 'No rule matched' }).label, 'No rule matched');
});

test('folder helpers', () => {
  const loc = { storageLocationId: 3, storageLocation: 'Disk 1', path: 'downloads/My Show' };
  assert.equal(folderLabel(loc), 'Disk 1 › downloads/My Show');
  assert.equal(folderLabel({ storageLocationId: 3, storageLocation: 'Disk 1', path: '' }), 'Disk 1');
  assert.equal(folderLabel({ path: '/elsewhere' }), '/elsewhere');
  assert.equal(filesHref(loc), '/files/3/downloads/My%20Show');
  assert.equal(filesHref({ path: '/x' }), null);
});

test('totals adds sizes and speeds', () => {
  assert.deepEqual(totals(torrents), { size: 65, down: 2048, up: 512 });
});
