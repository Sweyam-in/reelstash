import test from 'node:test';
import assert from 'node:assert/strict';
import { JOB_KINDS, RUNNABLE_KINDS, fileName, isActive, jobProgress, jobTitle, resultSummary } from './jobKinds.js';

test('every runnable kind has a title, description and icon', () => {
  assert.ok(RUNNABLE_KINDS.length >= 8);
  for (const kind of RUNNABLE_KINDS) {
    const info = JOB_KINDS[kind];
    assert.ok(info.title && info.description && info.icon, kind);
  }
});

test('only library-based kinds are marked perLibrary', () => {
  const perLibrary = RUNNABLE_KINDS.filter(k => JOB_KINDS[k].perLibrary).sort();
  assert.deepEqual(perLibrary, ['METADATA_MATCH', 'ORGANIZE', 'SCAN']);
});

test('jobTitle knows housekeeping kinds and falls back to the raw kind', () => {
  assert.equal(jobTitle('SCAN'), 'Scan library');
  assert.equal(jobTitle('THUMBNAIL'), 'Generate thumbnail');
  assert.equal(jobTitle('SOMETHING_NEW'), 'SOMETHING_NEW');
});

test('isActive is true only while pending or running', () => {
  assert.equal(isActive({ status: 'PENDING' }), true);
  assert.equal(isActive({ status: 'RUNNING' }), true);
  for (const status of ['COMPLETED', 'FAILED', 'CANCELLED']) assert.equal(isActive({ status }), false);
});

test('fileName keeps the last path segment', () => {
  assert.equal(fileName('a/b/c.mp4'), 'c.mp4');
  assert.equal(fileName('c.mp4'), 'c.mp4');
  assert.equal(fileName('folder/'), 'folder/');
  assert.equal(fileName(''), '');
  assert.equal(fileName(null), '');
});

test('a pending job is indeterminate and waiting', () => {
  assert.deepEqual(jobProgress({ status: 'PENDING' }), { percent: null, label: 'Waiting in queue', detail: '' });
});

test('a scan reports its phase and switches to a percentage once the walk is done', () => {
  const base = { kind: 'SCAN', status: 'RUNNING' };
  const walking = jobProgress({ ...base, scan: { phase: 'walking', filesSeen: 12, filesProcessed: 0, walkComplete: false, currentPath: 'x/y.mp4' } });
  assert.equal(walking.percent, null);
  assert.equal(walking.label, 'Searching the disk · 12 found');
  assert.equal(walking.detail, 'y.mp4');

  const indexing = jobProgress({ ...base, scan: { phase: 'indexing', filesSeen: 200, filesProcessed: 50, walkComplete: true, currentPath: 'a/b.mkv' } });
  assert.equal(indexing.percent, 25);
  assert.equal(indexing.label, 'Indexing files · 50 of 200');

  assert.equal(jobProgress({ ...base, scan: { phase: 'finishing', filesSeen: 10, filesProcessed: 10, walkComplete: true } }).percent, 100);
  assert.equal(jobProgress({ ...base, scan: { phase: 'queued', filesSeen: 0, walkComplete: false } }).label, 'Starting scan');
});

test('scan percentage never exceeds 100', () => {
  const p = jobProgress({ kind: 'SCAN', status: 'RUNNING', scan: { phase: 'indexing', filesSeen: 10, filesProcessed: 30, walkComplete: true } });
  assert.equal(p.percent, 100);
});

test('other jobs use their own progress counters', () => {
  const p = jobProgress({ kind: 'ORGANIZE', status: 'RUNNING', progressDone: 3, progressTotal: 12, progressLabel: 'dir/file.mp4' });
  assert.deepEqual(p, { percent: 25, label: '3 of 12', detail: 'file.mp4' });
  assert.equal(jobProgress({ kind: 'ORGANIZE', status: 'RUNNING', progressDone: 0, progressTotal: 0 }).label, 'Working…');
});

test('resultSummary explains failures, cancellations and running jobs', () => {
  assert.equal(resultSummary({ status: 'FAILED', error: 'disk offline' }), 'disk offline');
  assert.equal(resultSummary({ status: 'FAILED' }), 'Failed');
  assert.equal(resultSummary({ status: 'CANCELLED' }), 'Stopped by user');
  assert.equal(resultSummary({ status: 'RUNNING', kind: 'SCAN' }), '');
});

test('resultSummary summarises scan and match results', () => {
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'SCAN', scan: { filesSeen: 10, discovered: 3, changed: 2, missing: 1 } }), '10 files · 3 new · 2 changed · 1 missing');
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'METADATA_MATCH', result: { total: 8, matched: 5, failed: 1 } }), '5 of 8 looked up · 1 failed');
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'METADATA_MATCH', result: { total: 0 } }), 'Nothing to match');
});

test('resultSummary turns generic numeric results into words', () => {
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'DUPLICATE_SCAN', result: { groupsFound: 4, files: 9 } }), '4 groups found · 9 files');
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'X', result: { a: 1, b: 2, c: 3, d: 4 } }).split(' · ').length, 3);
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'X', result: {} }), 'Completed');
  assert.equal(resultSummary({ status: 'COMPLETED', kind: 'X' }), 'Completed');
});

test('resultSummary describes a missing-titles cleanup', () => {
  const done = { kind: 'REMOVE_MISSING', status: 'COMPLETED' };
  assert.equal(resultSummary({ ...done, result: { removedTitles: 17, removedFiles: 17, kept: 0, skippedLocationCount: 0, skippedLocations: [] } }), '17 titles removed · 17 missing files cleared');
  assert.match(resultSummary({ ...done, result: { removedTitles: 0, removedFiles: 0, kept: 2, skippedLocationCount: 1, skippedLocations: ['disk2'] } }), /2 back on disk · skipped disk2/);
});
