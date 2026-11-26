import test from 'node:test';
import assert from 'node:assert/strict';
import { describeCondition, describeRules, emptyRules, fromServerRules, newCondition, toServerRules, validateRules } from './smartRules.js';

test('a new condition starts with a sensible operator and value for its field', () => {
  assert.deepEqual(newCondition('rating'), { field: 'rating', op: 'gte', value: '4' });
  assert.deepEqual(newCondition('tag'), { field: 'tag', op: 'is', value: '' });
  assert.deepEqual(newCondition('watched'), { field: 'watched', value: false });
  assert.deepEqual(newCondition('kind'), { field: 'kind', value: 'movie' });
  assert.deepEqual(newCondition('addedWithin'), { field: 'addedWithin', value: '30' });
});

test('editor rules become what the server accepts', () => {
  const rules = {
    match: 'any', sort: 'rated', limit: '25',
    conditions: [
      { field: 'rating', op: 'gte', value: '4' },
      { field: 'tag', op: 'is', value: '  Marvel ' },
      { field: 'watched', value: false },
      { field: 'addedWithin', value: '30' },
    ],
  };
  assert.deepEqual(toServerRules(rules), {
    match: 'any', sort: 'rated', limit: 25,
    conditions: [
      { field: 'rating', op: 'gte', value: 4 },
      { field: 'tag', op: 'is', value: 'Marvel' },
      { field: 'watched', value: false },
      { field: 'addedWithin', value: 30 },
    ],
  });
  assert.equal('limit' in toServerRules({ ...rules, limit: 0 }), false, 'no limit is left out');
});

test('server rules round-trip through the editor', () => {
  const saved = { match: 'all', sort: 'added', conditions: [{ field: 'year', op: 'lte', value: 2005 }, { field: 'favorite' }, { field: 'studio', op: 'contains', value: 'Acme' }] };
  const editor = fromServerRules(saved);
  assert.deepEqual(editor.conditions, [{ field: 'year', op: 'lte', value: '2005' }, { field: 'favorite', value: true }, { field: 'studio', op: 'contains', value: 'Acme' }]);
  assert.deepEqual(toServerRules(editor).conditions, [{ field: 'year', op: 'lte', value: 2005 }, { field: 'favorite', value: true }, { field: 'studio', op: 'contains', value: 'Acme' }]);
  assert.deepEqual(fromServerRules(null), emptyRules());
});

test('validation names what to fix', () => {
  const ok = { match: 'all', sort: 'added', limit: 0, conditions: [{ field: 'tag', op: 'is', value: 'x' }] };
  assert.equal(validateRules(ok), '');
  assert.match(validateRules({ ...ok, conditions: [] }), /at least one/);
  assert.match(validateRules({ ...ok, conditions: [{ field: 'tag', op: 'is', value: '  ' }] }), /Tag/);
  assert.match(validateRules({ ...ok, conditions: [{ field: 'rating', op: 'gte', value: '9' }] }), /between 1 and 5/);
  assert.match(validateRules({ ...ok, conditions: [{ field: 'rating', op: 'gte', value: '' }] }), /whole number/);
  assert.match(validateRules({ ...ok, conditions: [{ field: 'addedAfter', value: 'soon' }] }), /date/);
  assert.match(validateRules({ ...ok, limit: 9999 }), /limit/);
  assert.match(validateRules({ ...ok, conditions: Array.from({ length: 21 }, () => ok.conditions[0]) }), /at most/);
});

test('conditions read as plain sentences', () => {
  assert.equal(describeCondition({ field: 'watched', value: false }), 'Not watched yet');
  assert.equal(describeCondition({ field: 'rating', op: 'gte', value: '4' }), 'My rating at least 4 stars');
  assert.equal(describeCondition({ field: 'tag', op: 'isNot', value: 'Marvel' }), 'Tag is not “Marvel”');
  assert.equal(describeCondition({ field: 'addedWithin', value: '30' }), 'Added in the last 30 days');
  assert.equal(describeCondition({ field: 'kind', value: 'episode' }), 'Type is episode');
  assert.equal(describeRules({ match: 'any', conditions: [{ field: 'favorite', value: true }, { field: 'year', op: 'eq', value: '1999' }] }), 'Is a favorite or Release year exactly 1999');
});
