import test from 'node:test';
import assert from 'node:assert/strict';
import { diskNameFromPath, folderProblem, folderSummary, modeInfo, scanPercent, scanSummary, SETUP_STEPS } from './setupFlow.js';

test('each mode suggests a library name and the provider that fits it', () => {
  assert.equal(modeInfo('entertainment').provider.id, 'tmdb');
  assert.equal(modeInfo('private').provider.plugin, true);
  assert.equal(modeInfo('entertainment').provider.plugin, undefined);
  assert.equal(modeInfo('nonsense').defaultName, 'Movies & TV');
  assert.deepEqual(SETUP_STEPS.map(s => s.id), ['mode', 'folder', 'metadata', 'scan']);
});

test('disk names come from the last folder', () => {
  assert.equal(diskNameFromPath('/mnt/media/Films/'), 'Films');
  assert.equal(diskNameFromPath('/'), 'Media');
  assert.equal(diskNameFromPath(''), 'Media');
});

test('a folder is usable only when it exists, is a readable folder', () => {
  assert.match(folderProblem(null), /first/);
  assert.equal(folderProblem({ exists: true, isDir: true, readable: true }), '');
  assert.equal(folderProblem({ exists: false, error: 'does not exist' }), 'does not exist');
  assert.match(folderProblem({ exists: true, isDir: false }), /cannot read/);
});

test('the folder summary says what a scan would find', () => {
  const ok = { exists: true, isDir: true, readable: true };
  assert.equal(folderSummary({ ...ok, videoCount: 1 }), '1 video found.');
  assert.equal(folderSummary({ ...ok, videoCount: 1234 }), `${(1234).toLocaleString()} videos found.`);
  assert.match(folderSummary({ ...ok, videoCount: 500, truncated: true }), /^At least 500/);
  assert.match(folderSummary({ ...ok, videoCount: 0 }), /No videos found yet/);
  assert.equal(folderSummary({ exists: false }), '');
});

test('scan progress reads from the scan row', () => {
  assert.equal(scanPercent(null), null);
  assert.equal(scanPercent({ walkComplete: false, filesSeen: 10 }), null);
  assert.equal(scanPercent({ walkComplete: true, filesSeen: 10, filesProcessed: 5 }), 50);
  assert.equal(scanPercent({ walkComplete: true, filesSeen: 10, filesProcessed: 10 }), 99);
  assert.equal(scanPercent({ completedAt: '2026-01-01' }), 100);
  assert.equal(scanSummary(null), 'Starting…');
  assert.match(scanSummary({ walkComplete: false, filesSeen: 7 }), /7 found so far/);
  assert.match(scanSummary({ walkComplete: true, filesSeen: 10, filesProcessed: 4 }), /4 of 10/);
  assert.equal(scanSummary({ completedAt: 'x', discoveredCount: 12 }), 'Finished. 12 videos added.');
  assert.equal(scanSummary({ completedAt: 'x', discoveredCount: 0 }), 'Finished. No videos were found.');
});
