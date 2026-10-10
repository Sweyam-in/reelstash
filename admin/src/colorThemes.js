// Color themes for the media library. The admin picks one; light and dark stay
// a personal choice. Ids must match mediaColorThemeIDs in the API.

export const DEFAULT_COLOR_THEME = 'grove';

export const COLOR_THEMES = [
  {
    id: 'grove',
    name: 'Grove',
    blurb: 'Orange into green, the same colors as the logo.',
    from: '#f26b1d',
    to: '#14956a',
    accent: '#f26b1d',
    accentText: '#ffc48a',
    accentLight: '#c24e0c',
    accentTextLight: '#9a3d09',
    tones: ['#f26b1d', '#ff9a3c', '#f5c330', '#22c998', '#0e9aa7', '#7bc142', '#e8a317', '#3dbe6e'],
  },
  {
    id: 'lagoon',
    name: 'Lagoon',
    blurb: 'Teal into deep blue, cool and clear.',
    from: '#14b8c8',
    to: '#2563eb',
    accent: '#14b8c8',
    accentText: '#a5f3fc',
    accentLight: '#0e7490',
    accentTextLight: '#155e75',
    tones: ['#14b8c8', '#38bdf8', '#67e8f9', '#2dd4bf', '#2563eb', '#22d3ee', '#60a5fa', '#0ea5e9'],
  },
  {
    id: 'ember',
    name: 'Ember',
    blurb: 'Flame into gold, warm without any green.',
    from: '#ff4d2e',
    to: '#f5b942',
    accent: '#ff4d2e',
    accentText: '#ffd0c2',
    accentLight: '#c2410c',
    accentTextLight: '#9a3412',
    tones: ['#ff4d2e', '#ff7a3c', '#f5b942', '#ff8a4c', '#e85d04', '#f59e0b', '#d97706', '#fb923c'],
  },
  {
    id: 'iris',
    name: 'Iris',
    blurb: 'Violet into blue, a late-night cinema look.',
    from: '#7c5cff',
    to: '#3b82f6',
    accent: '#7c5cff',
    accentText: '#ddd6fe',
    accentLight: '#5b21b6',
    accentTextLight: '#4c1d95',
    tones: ['#7c5cff', '#818cf8', '#a78bfa', '#60a5fa', '#4f46e5', '#38bdf8', '#8b5cf6', '#22d3ee'],
  },
  {
    id: 'aurora',
    name: 'Aurora',
    blurb: 'Mint into violet, like light on a dark sky.',
    from: '#2dd4a8',
    to: '#6d5ef7',
    accent: '#2dd4a8',
    accentText: '#bbf7e0',
    accentLight: '#0f766e',
    accentTextLight: '#115e59',
    tones: ['#2dd4a8', '#5eead4', '#a78bfa', '#34d399', '#6d5ef7', '#67e8f9', '#22c55e', '#818cf8'],
  },
];

export function colorThemeById(id) {
  return COLOR_THEMES.find(theme => theme.id === id) || COLOR_THEMES[0];
}

// Writes the palette onto the document so the media app, its portaled menus,
// and the login screen all inherit it. Grove stays the CSS fallback.
export function paintColorTheme(id) {
  const theme = colorThemeById(id);
  const root = document.documentElement;
  const vars = {
    '--rs-palette-from': theme.from,
    '--rs-palette-to': theme.to,
    '--rs-palette-accent': theme.accent,
    '--rs-palette-accent-text': theme.accentText,
    '--rs-palette-accent-light': theme.accentLight,
    '--rs-palette-accent-text-light': theme.accentTextLight,
    '--rs-brand': `linear-gradient(120deg, ${theme.from} 0%, ${theme.to} 100%)`,
  };
  theme.tones.forEach((color, index) => { vars[`--rs-palette-t${index}`] = color; });
  Object.entries(vars).forEach(([key, value]) => root.style.setProperty(key, value));
  root.dataset.colorTheme = theme.id;
  return theme;
}
