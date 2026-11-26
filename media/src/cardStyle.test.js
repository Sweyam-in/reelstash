import assert from 'node:assert/strict';
import test from 'node:test';

const store = new Map();
globalThis.localStorage = {
  getItem: key => (store.has(key) ? store.get(key) : null),
  setItem: (key, value) => store.set(key, String(value)),
};
globalThis.window = { dispatchEvent() {} };

const { cardMetrics, readCardStyle, saveCardStyle } = await import('./cardStyle.js');

test('title cards default to posters', () => {
  store.clear();
  assert.equal(readCardStyle(), 'poster');
  assert.deepEqual(cardMetrics('poster'), { aspect: 2 / 3, wideColumn: 150, narrowColumn: 118 });
});

test('thumbnail mode is one landscape size and stays saved', () => {
  saveCardStyle('thumbnail');
  assert.equal(readCardStyle(), 'thumbnail');
  assert.deepEqual(cardMetrics('thumbnail'), { aspect: 16 / 10, wideColumn: 210, narrowColumn: 145 });
  saveCardStyle('anything-else');
  assert.equal(readCardStyle(), 'poster');
});

test('grid density scales the card columns and defaults to comfortable', () => {
  const base = cardMetrics('poster');
  assert.deepEqual(cardMetrics('poster', 'comfortable'), base);
  assert.ok(cardMetrics('poster', 'compact').wideColumn < base.wideColumn);
  assert.ok(cardMetrics('poster', 'large').wideColumn > base.wideColumn);
  assert.deepEqual(cardMetrics('poster', 'unknown'), base);
  assert.equal(cardMetrics('thumbnail', 'large').aspect, cardMetrics('thumbnail').aspect);
});
