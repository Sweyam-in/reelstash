import test from 'node:test';
import assert from 'node:assert/strict';
import { BLACK_LUMA, clampLabel, formatClock, fractionAt, markerNear, meanLuma, spriteTile } from './playerUtils.js';

test('formatClock', () => {
  assert.equal(formatClock(0), '0:00');
  assert.equal(formatClock(83.9), '1:23');
  assert.equal(formatClock(3725), '1:02:05');
  assert.equal(formatClock(-4), '0:00');
  assert.equal(formatClock(NaN), '0:00');
  assert.equal(formatClock(Infinity), '0:00');
});

test('fractionAt clamps to the bar', () => {
  const rect = { left: 100, width: 200 };
  assert.equal(fractionAt(200, rect), 0.5);
  assert.equal(fractionAt(50, rect), 0);
  assert.equal(fractionAt(999, rect), 1);
  assert.equal(fractionAt(200, { left: 0, width: 0 }), 0);
});

test('spriteTile walks the grid row by row and clamps past the end', () => {
  const sprite = { intervalSeconds: 4, tileWidth: 160, tileHeight: 90, columns: 10, count: 19 };
  assert.deepEqual(spriteTile(sprite, 0), { x: 0, y: 0, width: 160, height: 90 });
  assert.deepEqual(spriteTile(sprite, 5), { x: 160, y: 0, width: 160, height: 90 });
  assert.deepEqual(spriteTile(sprite, 40), { x: 0, y: 90, width: 160, height: 90 });
  assert.deepEqual(spriteTile(sprite, 9999), { x: 1280, y: 90, width: 160, height: 90 });
  assert.deepEqual(spriteTile(sprite, -3), { x: 0, y: 0, width: 160, height: 90 });
});

test('markerNear picks the closest marker inside the tolerance', () => {
  const markers = [{ id: 1, seconds: 10 }, { id: 2, seconds: 14 }, { id: 3, seconds: 60 }];
  assert.equal(markerNear(markers, 13, 2).id, 2);
  assert.equal(markerNear(markers, 12, 3).id, 1); // ties resolve to the first
  assert.equal(markerNear(markers, 30, 2), null);
  assert.equal(markerNear([], 5, 5), null);
});

test('clampLabel keeps the label inside the bar', () => {
  assert.equal(clampLabel(5, 160, 600), 0);
  assert.equal(clampLabel(300, 160, 600), 220);
  assert.equal(clampLabel(590, 160, 600), 440);
  assert.equal(clampLabel(50, 160, 100), 0);
});

test('meanLuma tells black from picture', () => {
  const black = new Uint8ClampedArray(4 * 16).fill(0);
  for (let i = 3; i < black.length; i += 4) black[i] = 255;
  const bright = new Uint8ClampedArray(4 * 16).fill(200);
  assert.ok(meanLuma(black) < BLACK_LUMA);
  assert.ok(meanLuma(bright) > BLACK_LUMA);
  assert.equal(meanLuma(new Uint8ClampedArray(0)), 0);
});
