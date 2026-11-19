import test from 'node:test';
import assert from 'node:assert/strict';
import { initials, isLowSpace, plural, usedFraction, userLabel } from './format.js';

test('plural counts with a thousands separator and the right word', () => {
  assert.equal(plural(1, 'job'), '1 job');
  assert.equal(plural(2, 'job'), '2 jobs');
  assert.equal(plural(1, 'person', 'people'), '1 person');
  assert.equal(plural(0, 'person', 'people'), '0 people');
});

test('disk usage is a fraction, and low space means under 10% free', () => {
  assert.equal(usedFraction({ totalCapacity: 0 }), null);
  assert.equal(usedFraction({ totalCapacity: 100, usedCapacity: 25 }), 0.25);
  assert.equal(isLowSpace({ totalCapacity: 100, availableCapacity: 9 }), true);
  assert.equal(isLowSpace({ totalCapacity: 100, availableCapacity: 10 }), false);
  assert.equal(isLowSpace({}), false);
});

test('people are named from what the account has', () => {
  assert.equal(userLabel({ id: 4, displayName: 'Asha Nair' }), 'Asha Nair');
  assert.equal(userLabel({ id: 4, username: 'asha' }), 'asha');
  assert.equal(userLabel({ id: 4, email: 'ravi@example.com' }), 'ravi');
  assert.equal(userLabel({ id: 4 }), 'User 4');
  assert.equal(initials('Asha Nair', 'a@b.c'), 'AN');
  assert.equal(initials('', 'ravi@example.com'), 'RA');
});
