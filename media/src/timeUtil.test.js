import test from 'node:test';
import assert from 'node:assert/strict';
import { formatDuration, parseTs, timeAgo } from './pages/timeUtil.js';

const NOW = Date.parse('2026-09-30T12:00:00Z');

test('parseTs reads Postgres text timestamps', () => {
  assert.equal(parseTs('2026-09-28 03:28:54.1+00').toISOString(), '2026-09-28T03:28:54.100Z');
  assert.equal(parseTs('2026-09-28 03:28:54+00').toISOString(), '2026-09-28T03:28:54.000Z');
  assert.equal(parseTs('2026-09-28T03:28:54Z').toISOString(), '2026-09-28T03:28:54.000Z');
});

test('parseTs returns null for empty or invalid input', () => {
  for (const bad of [null, undefined, '', 'not a date']) assert.equal(parseTs(bad), null);
});

test('timeAgo buckets from seconds up to days', () => {
  const ago = seconds => timeAgo(new Date(NOW - seconds * 1000).toISOString(), NOW);
  assert.equal(ago(0), 'just now');
  assert.equal(ago(9), 'just now');
  assert.equal(ago(10), '10s ago');
  assert.equal(ago(59), '59s ago');
  assert.equal(ago(60), '1m ago');
  assert.equal(ago(45 * 60), '45m ago');
  assert.equal(ago(3600), '1h ago');
  assert.equal(ago(5 * 3600), '5h ago');
  assert.equal(ago(86400), '1d ago');
  assert.equal(ago(10 * 86400), '10d ago');
});

test('timeAgo past a month prints the calendar date', () => {
  const old = new Date(NOW - 90 * 86400 * 1000).toISOString();
  assert.equal(timeAgo(old, NOW), new Date(old).toLocaleDateString());
});

test('timeAgo never goes negative for clock skew', () => {
  assert.equal(timeAgo(new Date(NOW + 60000).toISOString(), NOW), 'just now');
});

test('timeAgo shows a dash for a missing time', () => {
  assert.equal(timeAgo(null), '—');
  assert.equal(timeAgo('garbage'), '—');
});

test('formatDuration renders seconds, minutes and hours', () => {
  assert.equal(formatDuration(0), '0s');
  assert.equal(formatDuration(59.4), '59s');
  assert.equal(formatDuration(60), '1m 0s');
  assert.equal(formatDuration(125), '2m 5s');
  assert.equal(formatDuration(3600), '1h 0m');
  assert.equal(formatDuration(3 * 3600 + 25 * 60 + 40), '3h 25m');
});

test('formatDuration shows a dash for missing values', () => {
  for (const bad of [null, undefined, NaN, Infinity]) assert.equal(formatDuration(bad), '—');
});
