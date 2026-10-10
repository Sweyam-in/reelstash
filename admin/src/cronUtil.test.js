import test from 'node:test';
import assert from 'node:assert/strict';
import { CRON_PRESETS, describeCron, jobKindLabel, looksLikeCron, presetFor } from './cronUtil.js';

test('presets are recognised whatever the spacing', () => {
  assert.equal(presetFor('0 3 * * *').id, 'nightly');
  assert.equal(presetFor('  0   3 * * * ').id, 'nightly');
  assert.equal(presetFor('5 3 * * *'), null);
  assert.equal(describeCron('0 3 * * 0'), 'Every Sunday at 03:00');
  assert.equal(describeCron('*/10 * * * *'), '*/10 * * * *');
});

test('every preset is a five-field expression', () => {
  for (const preset of CRON_PRESETS) assert.ok(looksLikeCron(preset.cron), preset.cron);
});

test('a quick shape check catches the obvious mistakes', () => {
  assert.equal(looksLikeCron('0 3 * * *'), true);
  assert.equal(looksLikeCron('@daily'), true);
  assert.equal(looksLikeCron('every day'), false);
  assert.equal(looksLikeCron('0 3 * *'), false);
  assert.equal(looksLikeCron(''), false);
});

test('job kinds have names', () => {
  assert.equal(jobKindLabel('SCAN'), 'Scan for new files');
  assert.equal(jobKindLabel('OTHER'), 'OTHER');
});
