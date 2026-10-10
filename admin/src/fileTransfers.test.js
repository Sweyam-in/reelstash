import test from 'node:test';
import assert from 'node:assert/strict';
import { overallPercent, transferProgress, transferTitle, transferToast } from './fileTransfers.js';

const job = (over = {}) => ({ id: 1, kind: 'FILE_TRANSFER', status: 'RUNNING', progressDone: 0, progressTotal: 0, payload: { mode: 'move', items: ['a.mkv', 'b.mkv'], destPath: 'Movies/New' }, ...over });

test('titles name the action, the items and where they go', () => {
  assert.equal(transferTitle(job()), 'Moving 2 items to New');
  assert.equal(transferTitle(job({ payload: { mode: 'copy', items: ['a.mkv'], destPath: '' } })), 'Copying a.mkv');
  assert.equal(transferTitle(job({ status: 'COMPLETED' })), 'Moved 2 items to New');
  assert.equal(transferTitle(job({ status: 'FAILED' })), 'Move failed');
});

test('progress is a percentage of KiB with a byte label', () => {
  assert.deepEqual(transferProgress(job({ status: 'PENDING' })).percent, null);
  const p = transferProgress(job({ progressDone: 512, progressTotal: 2048, progressLabel: 'a.mkv' }));
  assert.equal(p.percent, 25);
  assert.match(p.detail, /a\.mkv$/);
  assert.equal(transferProgress(job({ progressTotal: 0 })).percent, null);
});

test('overall percent weighs running transfers by size', () => {
  assert.equal(overallPercent([job({ progressDone: 100, progressTotal: 100 }), job({ progressDone: 0, progressTotal: 300 })]), 25);
  assert.equal(overallPercent([job({ status: 'PENDING' })]), null);
});

test('finished toasts flag partial failures', () => {
  assert.equal(transferToast(job({ status: 'COMPLETED', result: { transferred: 2, failures: [] } })).tone, 'success');
  const partial = transferToast(job({ status: 'COMPLETED', result: { transferred: 1, failures: ['b.mkv: exists'] } }));
  assert.equal(partial.tone, 'warn');
  assert.match(partial.description, /b\.mkv/);
  assert.equal(transferToast(job({ status: 'FAILED', error: 'disk full' })).description, 'disk full');
});
