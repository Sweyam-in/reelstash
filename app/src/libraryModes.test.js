import test from 'node:test';
import assert from 'node:assert/strict';
import { LIBRARY_MODES, modeTerms } from './libraryModes.js';

test('there is a choice for each library mode the server accepts', () => {
  assert.deepEqual(LIBRARY_MODES.map(m => m.id).sort(), ['entertainment', 'private']);
  for (const mode of LIBRARY_MODES) assert.ok(mode.label && mode.description);
});

test('entertainment speaks in movies, shows, cast and genres', () => {
  const t = modeTerms('entertainment');
  assert.equal(t.movies, 'Movies');
  assert.equal(t.show, 'TV show');
  assert.equal(t.person, 'Actor');
  assert.equal(t.category, 'Genre');
  assert.equal(t.series, 'Collection');
});

test('private keeps the original video vocabulary', () => {
  const t = modeTerms('private');
  assert.equal(t.title, 'Video');
  assert.equal(t.person, 'Performer');
  assert.equal(t.category, 'Category');
  assert.equal(t.series, 'Series');
});

test('an unknown or missing mode falls back to private terms', () => {
  assert.equal(modeTerms(undefined), modeTerms('private'));
  assert.equal(modeTerms('nonsense'), modeTerms('private'));
});

test('both vocabularies define every term the pages read', () => {
  const required = ['title', 'titles', 'library', 'person', 'people', 'cast', 'studio', 'studios', 'series', 'seriesPlural', 'category', 'categories', 'searchPlaceholder'];
  for (const id of ['entertainment', 'private']) {
    for (const key of required) assert.ok(modeTerms(id)[key], `${id} is missing "${key}"`);
  }
});
