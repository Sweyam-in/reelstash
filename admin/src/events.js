import { useEffect, useRef, useState } from 'react';

/**
 * One shared server-sent-events connection (/api/events) for the whole app.
 * Components subscribe to named events; the connection opens with the first
 * subscriber and closes with the last. EventSource reconnects on its own, and
 * the server re-sends a fresh snapshot on every connect.
 */
const listeners = new Map(); // event name -> Set<fn>
const statusListeners = new Set();
let source = null;
let connected = false;

function setConnected(value) {
  if (connected === value) return;
  connected = value;
  statusListeners.forEach(fn => fn(value));
}

function forward(name) {
  return event => {
    let data = null;
    try { data = JSON.parse(event.data); } catch { return; }
    listeners.get(name)?.forEach(fn => fn(data));
  };
}

function open() {
  if (source || typeof EventSource === 'undefined') return;
  source = new EventSource('/api/events', { withCredentials: true });
  source.addEventListener('ready', () => setConnected(true));
  source.onerror = () => setConnected(false);
  for (const name of listeners.keys()) source.addEventListener(name, forward(name));
}

function closeIfIdle() {
  const anyone = [...listeners.values()].some(set => set.size > 0);
  if (anyone || !source) return;
  source.close();
  source = null;
  setConnected(false);
}

export function subscribe(name, fn) {
  let set = listeners.get(name);
  if (!set) {
    set = new Set();
    listeners.set(name, set);
    if (source) source.addEventListener(name, forward(name));
  }
  set.add(fn);
  open();
  return () => { set.delete(fn); closeIfIdle(); };
}

/** Runs handler for every event with this name while the component is mounted. A falsy name subscribes to nothing. */
export function useServerEvent(name, handler) {
  const latest = useRef(handler);
  latest.current = handler;
  useEffect(() => (name ? subscribe(name, data => latest.current(data)) : undefined), [name]);
}

/** True while the live connection is up (so callers can fall back to polling when it is not). */
export function useEventsConnected() {
  const [value, setValue] = useState(connected);
  useEffect(() => {
    statusListeners.add(setValue);
    setValue(connected);
    return () => { statusListeners.delete(setValue); };
  }, []);
  return value;
}
