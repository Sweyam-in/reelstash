import { useState } from 'react';

function read(storageKey) {
  try {
    const raw = localStorage.getItem(storageKey);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed : [];
  } catch { return []; }
}

// Named filter presets kept in this browser, namespaced per page.
export function useFilterPresets(storageKey) {
  const [presets, setPresets] = useState(() => read(storageKey));
  const persist = next => {
    setPresets(next);
    try { localStorage.setItem(storageKey, JSON.stringify(next)); } catch { /* private mode: views are a convenience */ }
  };
  return {
    presets,
    savePreset: (name, filters) => persist([...presets.filter(p => p.name !== name), { name, filters }]),
    deletePreset: name => persist(presets.filter(p => p.name !== name)),
  };
}

// Which optional columns of a table are shown, remembered per table.
export function useColumnVisibility(storageKey, columns) {
  const [hidden, setHidden] = useState(() => {
    try {
      const saved = JSON.parse(localStorage.getItem(storageKey) || 'null');
      if (Array.isArray(saved)) return saved;
    } catch { /* fall through */ }
    return columns.filter(c => c.defaultHidden).map(c => c.id);
  });
  const toggle = id => setHidden(current => {
    const next = current.includes(id) ? current.filter(x => x !== id) : [...current, id];
    try { localStorage.setItem(storageKey, JSON.stringify(next)); } catch { /* ignore */ }
    return next;
  });
  return { isVisible: id => !hidden.includes(id), toggle, hidden };
}
