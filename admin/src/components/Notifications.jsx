import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { useToast } from '@spatika/react';
import { getNotifications, markNotificationsRead } from '../api';
import { useServerEvent } from '../events';

const NotificationsContext = createContext({ items: [], unread: 0, markRead: () => {} });

/**
 * Server notifications (scan finished, metadata matched, job failed, ...): loaded once,
 * topped up live over the event stream, and announced with a toast when one arrives.
 */
export function NotificationsProvider({ enabled, children }) {
  const { toast } = useToast();
  const [items, setItems] = useState([]);
  const [unread, setUnread] = useState(0);

  useEffect(() => {
    if (!enabled) { setItems([]); setUnread(0); return undefined; }
    let live = true;
    getNotifications(15).then(result => { if (live) { setItems(result.items || []); setUnread(result.unread || 0); } }).catch(() => {});
    return () => { live = false; };
  }, [enabled]);

  useServerEvent(enabled ? 'notification' : null, note => {
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
