import test from 'node:test';
import assert from 'node:assert/strict';
import { monogram } from './Monogram.js';

test('two or more words use the first and last initials', () => {
  assert.equal(monogram('Ann Lee').initials, 'AL');
  assert.equal(monogram('  ann   marie   lee ').initials, 'AL');
});

test('a single word uses its first two letters', () => {
  assert.equal(monogram('Madonna').initials, 'MA');
  assert.equal(monogram('x').initials, 'X');
});

test('nothing at all reads as a question mark', () => {
  for (const blank of ['', '   ', undefined]) assert.equal(monogram(blank).initials, '?');
});

test('the colour is stable per name, case-insensitive, and drawn from the palette', () => {
  const a = monogram('Ann Lee').style['--mg-h'];
  assert.equal(monogram('ann lee').style['--mg-h'], a);
  assert.equal(monogram('Ann Lee').style['--mg-h'], a);
  assert.ok([8, 24, 44, 150, 172, 198, 218, 244, 268, 292, 322, 344].includes(a));
});

test('different names spread across several hues', () => {
  const hues = new Set(['Ann', 'Bob', 'Cara', 'Dan', 'Eve', 'Finn', 'Gus', 'Hana', 'Ivy', 'Jon'].map(n => monogram(n).style['--mg-h']));
  assert.ok(hues.size >= 4, `only ${hues.size} distinct hues`);
});
