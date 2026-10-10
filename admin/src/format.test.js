import test from 'node:test';
import assert from 'node:assert/strict';
import { formatBytes, formatResolution } from './format.js';

test('formatBytes scales through the units with sensible precision', () => {
  assert.equal(formatBytes(0), '0 B');
  assert.equal(formatBytes(1), '1 B');
  assert.equal(formatBytes(1023), '1023 B');
  assert.equal(formatBytes(1024), '1.0 KB');
  assert.equal(formatBytes(1536), '1.5 KB');
  assert.equal(formatBytes(1024 ** 2), '1.0 MB');
  assert.equal(formatBytes(5.5 * 1024 ** 3), '5.5 GB');
  assert.equal(formatBytes(2 * 1024 ** 4), '2.0 TB');
});

test('formatBytes caps at TB rather than inventing units', () => {
  assert.equal(formatBytes(3 * 1024 ** 5), '3072.0 TB');
});

test('formatBytes shows a dash for missing or non-finite values', () => {
  for (const bad of [null, undefined, NaN, Infinity, -Infinity]) assert.equal(formatBytes(bad), '—');
});

test('formatResolution names common heights and passes odd ones through', () => {
  assert.equal(formatResolution(2160), '4K');
  assert.equal(formatResolution(4320), '4K');
  assert.equal(formatResolution(1440), '1440p');
  assert.equal(formatResolution(1080), '1080p');
  assert.equal(formatResolution(720), '720p');
  assert.equal(formatResolution(480), '480p');
  assert.equal(formatResolution(360), '360p');
  assert.equal(formatResolution(1200), '1080p');
});

test('formatResolution shows a dash when the height is unknown', () => {
  for (const bad of [0, null, undefined]) assert.equal(formatResolution(bad), '—');
});
