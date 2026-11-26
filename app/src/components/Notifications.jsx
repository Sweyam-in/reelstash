import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useToast } from '../ui';
import { getNotifications, markNotificationsRead } from '../api';
import { useServerEvent } from '../events';

const NotificationsContext = createContext({ items: [], unread: 0, markRead: () => {} });

// Alerts that belong in the admin console (scans, jobs, downloads), even if a
// response still includes them.
const adminNotice = note => typeof note?.link === 'string' && /^\/(admin|files|downloads)(\/|$)/.test(note.link);

/**
 * Library notifications for the media app. Admin alerts (scans, jobs, downloads)
 * are not loaded or pushed here; they stay in the admin console. What does arrive
 * is loaded once, topped up live, and announced with a toast.
 */
export function NotificationsProvider({ enabled, children }) {
  const { toast } = useToast();
  const [items, setItems] = useState([]);
  const [unread, setUnread] = useState(0);

  useEffect(() => {
    if (!enabled) { setItems([]); setUnread(0); return undefined; }
    let live = true;
    getNotifications(15).then(result => { if (live) { setItems((result.items || []).filter(note => !adminNotice(note))); setUnread(result.unread || 0); } }).catch(() => {});
    return () => { live = false; };
  }, [enabled]);

  useServerEvent(enabled ? 'notification' : null, note => {
    if (adminNotice(note)) return;
    setItems(current => (current.some(item => item.id === note.id) ? current : [note, ...current].slice(0, 15)));
    setUnread(count => count + 1);
    toast({ title: note.title, description: note.body || undefined, tone: note.kind.endsWith('.failed') ? 'danger' : 'success' });
  });

  const unreadRef = useRef(0);
  useEffect(() => { unreadRef.current = unread; }, [unread]);
  const markRead = useCallback(() => {
    if (!enabled || unreadRef.current === 0) return;
    markNotificationsRead().catch(() => {});
    setUnread(0);
  }, [enabled]);

  const value = useMemo(() => ({ items: enabled ? items : [], unread: enabled ? unread : 0, markRead }), [enabled, items, unread, markRead]);
  return <NotificationsContext.Provider value={value}>{children}</NotificationsContext.Provider>;
}

export const useNotifications = () => useContext(NotificationsContext);
