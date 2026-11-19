import test from 'node:test';
import assert from 'node:assert/strict';
import { describeError, isForbiddenError } from './errors.js';

const err = (message, status) => Object.assign(new Error(message), status ? { status } : {});

test('errors say what failed and what to do next', () => {
  assert.equal(describeError('start the scan', err('library has no folders')), 'Couldn’t start the scan. Library has no folders.'.replace('’', "'"));
  assert.match(describeError('save', err('Failed to fetch')), /unreachable/);
  assert.match(describeError('save', err('x', 403)), /permission/);
  assert.match(describeError('save', err('x', 423)), /locked/);
  assert.match(describeError('save', undefined), /Try again/);
});

test('403 is recognised as a permission failure', () => {
  assert.equal(isForbiddenError(err('no', 403)), true);
  assert.equal(isForbiddenError(err('no', 500)), false);
  assert.equal(isForbiddenError(null), false);
});
