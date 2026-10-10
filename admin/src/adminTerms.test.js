import test from 'node:test';
import assert from 'node:assert/strict';
import { adminTerms } from './adminTerms.js';

test('admin terms are neutral across all libraries', () => {
  const t = adminTerms(null);
  assert.equal(t.people, 'People');
  assert.equal(t.seriesPlural, 'Collections / series');
  assert.equal(t.categories, 'Categories & genres');
});

test('admin terms follow the selected library mode', () => {
  assert.equal(adminTerms({ mode: 'entertainment' }).people, 'Actors');
  assert.equal(adminTerms({ mode: 'entertainment' }).categories, 'Genres');
  assert.equal(adminTerms({ mode: 'private' }).people, 'Performers');
  assert.equal(adminTerms({ mode: 'private' }).seriesPlural, 'Series');
});
