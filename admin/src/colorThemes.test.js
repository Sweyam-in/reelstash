import test from 'node:test';
import assert from 'node:assert/strict';
import { COLOR_THEMES, DEFAULT_COLOR_THEME, colorThemeById } from './colorThemes.js';

test('the library offers at least five distinct color themes', () => {
  assert.ok(COLOR_THEMES.length >= 5);
  assert.equal(new Set(COLOR_THEMES.map(theme => theme.id)).size, COLOR_THEMES.length);
  assert.equal(COLOR_THEMES[0].id, DEFAULT_COLOR_THEME);
  for (const theme of COLOR_THEMES) {
    assert.equal(theme.tones.length, 8);
    assert.match(theme.from, /^#[0-9a-f]{6}$/);
    assert.match(theme.to, /^#[0-9a-f]{6}$/);
    assert.notEqual(theme.from.toLowerCase(), theme.to.toLowerCase());
  }
});

test('an unknown theme falls back to Grove', () => {
  assert.equal(colorThemeById('nope').id, 'grove');
  assert.equal(colorThemeById('iris').name, 'Iris');
});
