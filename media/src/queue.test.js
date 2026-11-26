import test from 'node:test';
import assert from 'node:assert/strict';
import {
  __resetQueueForTests, clearQueue, enqueue, nextInQueue, playNext, removeFromQueue, shuffled, startQueue, syncPlaying,
} from './queue.js';

const t = (id, title = `T${id}`) => ({ id, title, studioName: 'S' });
const ids = list => list.map(item => item.id);

// queue.js reads the store through getState only via hooks; tests observe it with nextInQueue and a re-start.
test('startQueue points at the first title and nextInQueue walks forward', () => {
  __resetQueueForTests();
  assert.equal(startQueue([t(1), t(2), t(3)]).id, 1);
  assert.equal(nextInQueue().id, 2);
  syncPlaying(2);
  assert.equal(nextInQueue().id, 3);
  syncPlaying(3);
  assert.equal(nextInQueue(), null);
});

test('startQueue with nothing returns null and queues nothing', () => {
  __resetQueueForTests();
  assert.equal(startQueue([]), null);
  assert.equal(nextInQueue(), null);
});

test('enqueue ignores duplicates and appends at the end', () => {
  __resetQueueForTests();
  startQueue([t(1)]);
  assert.equal(enqueue(t(2)), true);
  assert.equal(enqueue(t(2)), false);
  assert.equal(nextInQueue().id, 2);
});

test('playNext inserts right after the playing title and moves an existing copy', () => {
  __resetQueueForTests();
  startQueue([t(1), t(2), t(3)]);
  assert.equal(playNext(t(3)), true); // 1, 3, 2
  assert.equal(nextInQueue().id, 3);
  assert.equal(playNext(t(1)), false); // that is the one playing
  assert.equal(playNext(t(9)), true); // 1, 9, 3, 2
  assert.equal(nextInQueue().id, 9);
});

test('removeFromQueue keeps the playing title selected', () => {
  __resetQueueForTests();
  startQueue([t(1), t(2), t(3)]);
  syncPlaying(2);
  removeFromQueue(1); // before the playing one: index must shift
  assert.equal(nextInQueue().id, 3);
  removeFromQueue(3);
  assert.equal(nextInQueue(), null);
});

test('syncPlaying ignores titles that are not queued', () => {
  __resetQueueForTests();
  startQueue([t(1), t(2)]);
  syncPlaying(99);
  assert.equal(nextInQueue().id, 2);
});

test('clearQueue empties everything', () => {
  __resetQueueForTests();
  startQueue([t(1), t(2)]);
  clearQueue();
  assert.equal(nextInQueue(), null);
});

test('shuffled returns a permutation without touching the input', () => {
  const input = [1, 2, 3, 4, 5, 6, 7, 8];
  const out = shuffled(input, (() => { let n = 0; return () => ((n += 0.37) % 1); })());
  assert.deepEqual([...out].sort(), input);
  assert.deepEqual(input, [1, 2, 3, 4, 5, 6, 7, 8]);
  assert.notDeepEqual(ids(out.map(id => ({ id }))), input);
});
