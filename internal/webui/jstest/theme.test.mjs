import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

const document = installStubDom();
const { applyTheme, contrastOn, shade, withAlpha } = await import('../assets/js/theme.js');
const { themeNames } = await import('../assets/js/views/settings.js');

// internal/webui/webui.go pins the other end (ThemeNames). Drift means the picker
// offers a theme the server will not render, or hides one it will.
const EXPECTED_THEMES = [
  'auto', 'light', 'dark',
  'catppuccin-latte', 'catppuccin-mocha', 'dracula', 'nord', 'gruvbox-light', 'gruvbox-dark',
  'custom',
];

const ACCENT_PROPERTIES = ['--accent', '--accent-contrast', '--accent-hover', '--accent-soft'];

test('the picker offers every theme the server can render', () => {
  assert.deepEqual(themeNames(), EXPECTED_THEMES);
});

test('applyTheme writes the theme onto the root element', () => {
  applyTheme({ theme: 'nord', theme_base: 'dark', theme_surface_tint: 'cool' });
  const { dataset } = document.documentElement;
  assert.equal(dataset.theme, 'nord');
  assert.equal(dataset.themeBase, 'dark');
  assert.equal(dataset.themeTint, 'cool');
});

// A named palette owns its accent. An inline override would win over the stylesheet
// and flatten every theme to the same colour.
test('a named theme sets no inline accent', () => {
  applyTheme({ theme: 'nord', theme_accent: '#ff8800' });
  for (const property of ACCENT_PROPERTIES) {
    assert.equal(document.documentElement.style.getPropertyValue(property), '', property);
  }
});

// The regression that matters: leaving the override behind after switching away from
// custom keeps it winning over the stylesheet.
test('switching from custom to a named theme clears the accent', () => {
  applyTheme({ theme: 'custom', theme_accent: '#ff8800' });
  assert.equal(document.documentElement.style.getPropertyValue('--accent'), '#ff8800');

  applyTheme({ theme: 'dracula', theme_accent: '#ff8800' });
  for (const property of ACCENT_PROPERTIES) {
    assert.equal(document.documentElement.style.getPropertyValue(property), '', `${property} was left behind`);
  }
});

test('a custom theme derives all four accent tokens', () => {
  applyTheme({ theme: 'custom', theme_accent: '#ff8800' });
  const style = document.documentElement.style;
  assert.equal(style.getPropertyValue('--accent'), '#ff8800');
  assert.equal(style.getPropertyValue('--accent-contrast'), contrastOn('#ff8800'));
  assert.equal(style.getPropertyValue('--accent-hover'), shade('#ff8800', -0.12));
  assert.equal(style.getPropertyValue('--accent-soft'), withAlpha('#ff8800', 0.16));
});

test('contrastOn picks readable text for either accent', () => {
  assert.equal(contrastOn('#f8e71c'), '#0d1117', 'a light accent needs dark text');
  assert.equal(contrastOn('#1b1b1b'), '#ffffff', 'a dark accent needs white text');
  assert.equal(contrastOn('nonsense'), '#ffffff', 'an unparseable accent falls back to white');
});

test('shade clamps instead of wrapping around', () => {
  assert.equal(shade('#ffffff', 0.5), '#ffffff');
  assert.equal(shade('#000000', -0.5), '#000000');
  assert.equal(shade('nonsense', -0.1), 'nonsense', 'an unparseable colour is passed through');
});

test('withAlpha expands short hex and passes junk through', () => {
  assert.equal(withAlpha('#f80', 0.5), 'rgba(255, 136, 0, 0.5)');
  assert.equal(withAlpha('not a colour', 0.5), 'not a colour');
});
