import React, { useEffect, useLayoutEffect, useRef } from 'react';

// A right-click menu for the file browser. items: { label, icon, shortcut, onSelect, danger, disabled } or 'sep'.
// It closes on outside click, Escape, scroll or resize, and supports arrow-key navigation.
export default function FileContextMenu({ x, y, items, onClose }) {
  const ref = useRef(null);

  // The menu is position:fixed, but an ancestor with a transform/filter/contain makes that relative to
  // the ancestor instead of the viewport. Place it, measure where it really landed, then correct for
  // the difference (and keep it inside the window).
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const place = (left, top) => { el.style.left = `${left}px`; el.style.top = `${top}px`; };
    place(x, y);
    const box = el.getBoundingClientRect();
    const offsetX = box.left - x;
    const offsetY = box.top - y;
    const left = Math.max(8, Math.min(x, window.innerWidth - box.width - 8));
    const top = Math.max(8, Math.min(y, window.innerHeight - box.height - 8));
    place(left - offsetX, top - offsetY);
  }, [x, y, items]);

  useEffect(() => {
    const buttons = () => Array.from(ref.current?.querySelectorAll('button:not(:disabled)') || []);
    buttons()[0]?.focus();
    const outside = event => { if (!ref.current?.contains(event.target)) onClose(); };
    const key = event => {
      if (event.key === 'Escape') { event.preventDefault(); onClose(); return; }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault();
        const list = buttons();
        if (!list.length) return;
        const at = list.indexOf(document.activeElement);
        const step = event.key === 'ArrowDown' ? 1 : -1;
        list[(at + step + list.length) % list.length].focus();
      }
    };
    document.addEventListener('pointerdown', outside, true);
    document.addEventListener('keydown', key, true);
    window.addEventListener('resize', onClose);
    window.addEventListener('blur', onClose);
    window.addEventListener('scroll', onClose, true);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      document.removeEventListener('keydown', key, true);
      window.removeEventListener('resize', onClose);
      window.removeEventListener('blur', onClose);
      window.removeEventListener('scroll', onClose, true);
    };
  }, [onClose]);

  return <div ref={ref} className="rs-file-context" role="menu" onContextMenu={event => { event.preventDefault(); event.stopPropagation(); }}>
    {items.map((item, index) => item === 'sep'
      ? <div key={`sep${index}`} className="rs-file-menu-rule" role="separator" />
      : <button key={item.label} type="button" role="menuitem" disabled={item.disabled} className={item.danger ? 'is-danger' : ''}
        onClick={() => { onClose(); item.onSelect(); }}>
        <span className="rs-file-context-icon">{item.icon}</span>
        <span className="rs-file-context-label">{item.label}</span>
        {item.shortcut && <kbd>{item.shortcut}</kbd>}
      </button>)}
  </div>;
}
