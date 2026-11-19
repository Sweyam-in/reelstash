import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { CheckCircle2, CircleAlert, Info, TriangleAlert, X } from 'lucide-react';
import { cx } from './overlay';

// Toast notifications: toast({ title, description, tone, action, duration }). They live in one polite
// live region so screen readers announce them without stealing focus.

const ToastContext = createContext(null);
const ICONS = { success: CheckCircle2, danger: CircleAlert, warning: TriangleAlert, info: Info };

let nextId = 1;

export function Toaster({ position = 'bottom-center', children }) {
  const [items, setItems] = useState([]);
  const timers = useRef(new Map());
  const dismiss = useCallback(id => {
    setItems(list => list.filter(item => item.id !== id));
    clearTimeout(timers.current.get(id));
    timers.current.delete(id);
  }, []);
  const toast = useCallback(({ title, description, tone = 'info', action, duration } = {}) => {
    const id = nextId++;
    setItems(list => [...list.slice(-3), { id, title, description, tone, action }]);
    const ms = duration ?? (tone === 'danger' ? 7000 : 4500);
    if (ms !== Infinity) timers.current.set(id, setTimeout(() => dismiss(id), ms));
    return id;
  }, [dismiss]);
  useEffect(() => () => timers.current.forEach(clearTimeout), []);
  const value = useMemo(() => ({ toast, dismiss }), [toast, dismiss]);
  const region = <div className={cx('rs-toaster', `rs-toaster--${position}`)} aria-live="polite" aria-label="Notifications" role="region">
    {items.map(item => {
      const Icon = ICONS[item.tone] || Info;
      return <div key={item.id} className={cx('rs-toast', `rs-toast--${item.tone}`)}>
        <Icon size={18} className="rs-toast-icon" aria-hidden="true" />
        <div className="rs-toast-text"><strong>{item.title}</strong>{item.description && <span>{item.description}</span>}</div>
        {item.action && <button type="button" className="rs-toast-action" onClick={() => { item.action.onClick?.(); dismiss(item.id); }}>{item.action.label}</button>}
        <button type="button" className="rs-toast-close" aria-label="Dismiss" onClick={() => dismiss(item.id)}><X size={14} /></button>
      </div>;
    })}
  </div>;
  return <ToastContext.Provider value={value}>
    {children}
    {typeof document !== 'undefined' ? createPortal(region, document.body) : null}
  </ToastContext.Provider>;
}

const fallback = { toast: () => 0, dismiss: () => {} };
export const useToast = () => useContext(ToastContext) || fallback;
