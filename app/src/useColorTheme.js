import { useEffect, useState } from 'react';
import { getAuthConfig } from './api';
import { DEFAULT_COLOR_THEME, colorThemeById, paintColorTheme } from './colorThemes';

// The admin's library palette. Refetched when the window is focused so a
// change made in the admin console shows up without a manual reload.
export function useMediaColorTheme() {
  const [id, setId] = useState(DEFAULT_COLOR_THEME);
  useEffect(() => {
    let live = true;
    const load = () => getAuthConfig()
      .then(cfg => { if (live) setId(colorThemeById(cfg?.colorTheme).id); })
      .catch(() => {});
    load();
    window.addEventListener('focus', load);
    return () => { live = false; window.removeEventListener('focus', load); };
  }, []);
  useEffect(() => { paintColorTheme(id); }, [id]);
  return id;
}
