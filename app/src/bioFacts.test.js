import test from 'node:test';
import assert from 'node:assert/strict';
import { splitBio } from './bioFacts.js';

test('splitBio pulls label lines out as facts and keeps prose', () => {
  const { facts, prose } = splitBio('Born in Caracas.\n\nHair: Brown\n\nBody Hair: Heavy\nHeight: 5ft 8in / 173cm\n\nHe moved to Spain in 2015, where his career began.', ['height']);
  assert.deepEqual(facts, [{ label: 'Hair', value: 'Brown' }, { label: 'Body Hair', value: 'Heavy' }]);
  assert.equal(prose, 'Born in Caracas.\n\nHe moved to Spain in 2015, where his career began.');
});

test('splitBio leaves sentences that happen to contain a colon as prose', () => {
  const text = 'Note: he started performing in 2019 after years working as a personal trainer in Madrid and Barcelona.';
  assert.deepEqual(splitBio(text), { facts: [], prose: text });
});
