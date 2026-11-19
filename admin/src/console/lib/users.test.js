import test from 'node:test';
import assert from 'node:assert/strict';
import { accountGuard, signInMethod, userStatus } from './users.js';

const admin = (id, over = {}) => ({ id, role: 'ADMIN', disabled: false, lastLoginAt: '2026-01-01T00:00:00Z', passwordLogin: true, ...over });
const user = (id, over = {}) => ({ id, role: 'USER', disabled: false, lastLoginAt: null, passwordLogin: false, ...over });

test('status is never colour alone: each state has a label', () => {
  assert.equal(userStatus(user(1, { disabled: true })).label, 'Disabled');
  assert.equal(userStatus(user(1)).label, 'Never signed in');
  assert.equal(userStatus(admin(1)).label, 'Active');
  assert.equal(signInMethod(user(1)), 'Single sign-on');
});

test('you cannot disable, demote or delete the account you are signed in with', () => {
  const guard = accountGuard(admin(1), 2, 1);
  assert.ok(guard.isSelf && guard.cannotDisable && guard.cannotDemote && guard.cannotDelete);
  const other = accountGuard(admin(2), 2, 1);
  assert.equal(other.cannotDisable, null);
  assert.equal(other.cannotDemote, null);
  assert.equal(other.cannotDelete, null);
});

test('the last enabled administrator is protected, a plain user is not', () => {
  assert.ok(accountGuard(admin(1), 1, 3).lastAdmin);
  assert.ok(accountGuard(admin(1), 1, 3).cannotDelete);
  assert.equal(accountGuard(user(3), 1, 9).lastAdmin, false);
  assert.equal(accountGuard(admin(1), 2, 3).lastAdmin, false);
  assert.equal(accountGuard(admin(1, { disabled: true }), 1, 3).lastAdmin, false);
});
