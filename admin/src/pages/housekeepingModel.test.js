import test from 'node:test';
import assert from 'node:assert/strict';
import { findingsFor, plural, selectionTotals } from './housekeepingModel.js';

const report = (findings) => ({ findings });

test('findingsFor overlays a newer clean-up on the analysis, task by task', () => {
  const lastAnalyze = { finishedAt: '2026-10-01 10:00:00+00', result: report([{ task: 'a', count: 3 }, { task: 'b', count: 2 }]) };
  const lastCleanup = { finishedAt: '2026-10-01 11:00:00+00', result: report([{ task: 'a', count: 3 }]) };
  assert.deepEqual(Object.values(findingsFor({ lastAnalyze }).findings).map(f => f.mode), ['analysis', 'analysis']);
  const both = findingsFor({ lastAnalyze, lastCleanup }).findings;
  assert.equal(both.a.mode, 'cleanup');
  assert.equal(both.b.mode, 'analysis');
  const reanalyzed = findingsFor({ lastAnalyze: { ...lastAnalyze, finishedAt: '2026-10-01 12:00:00+00' }, lastCleanup }).findings;
  assert.equal(reanalyzed.a.mode, 'analysis');
  assert.equal(findingsFor({ lastAnalyze, lastCleanup: { finishedAt: '2026-10-01 12:00:00+00', status: 'FAILED' } }).findings.a.mode, 'analysis');
  assert.deepEqual(findingsFor({}), { findings: {}, analyzedAt: null });
});

test('selectionTotals sums only selected tasks still awaiting clean-up', () => {
  const findings = { a: { count: 2, bytes: 10, mode: 'analysis' }, b: { count: 5, bytes: 1, mode: 'cleanup' }, vacuum: { count: 99, mode: 'analysis' } };
  assert.deepEqual(selectionTotals(new Set(['a', 'b', 'vacuum', 'missing']), findings), { count: 2, bytes: 10 });
});

test('plural', () => {
  assert.equal(plural(1, 'item'), '1 item');
  assert.equal(plural(0, 'item'), '0 items');
  assert.equal(plural(1200, 'task'), '1,200 tasks');
});
