import { useEffect, useLayoutEffect, useRef, useState } from 'react';

// Shared plumbing for ReelStash's own overlays (dialogs, menus, popovers): class names, focus
// handling and anchored positioning. No third-party UI library.

export const cx = (...parts) => parts.flat().filter(Boolean).join(' ');

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export const focusables = root => (root ? [...root.querySelectorAll(FOCUSABLE)].filter(el => !el.closest('[inert]') && el.getAttribute('aria-hidden') !== 'true') : []);

// Moves focus into `ref` while `active`, keeps Tab inside it, and gives focus back on close.
export function useFocusTrap(ref, active, { autoFocus = true } = {}) {
  useEffect(() => {
    if (!active) return undefined;
    const previous = document.activeElement;
    const root = ref.current;
    if (autoFocus && root && !root.contains(document.activeElement)) {
      const preferred = root.querySelector('[data-autofocus], [autofocus]');
      (preferred || focusables(root)[0] || root).focus?.({ preventScroll: true });
    }
    const onKey = event => {
      if (event.key !== 'Tab' || !ref.current) return;
      const items = focusables(ref.current);
      if (!items.length) { event.preventDefault(); return; }
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      if (previous && typeof previous.focus === 'function' && document.contains(previous)) previous.focus({ preventScroll: true });
    };
  }, [active, autoFocus, ref]);
}

// Esc closes the topmost overlay only.
const escapeStack = [];
export function useEscape(active, onEscape) {
  const handler = useRef(onEscape);
  handler.current = onEscape;
  useEffect(() => {
    if (!active) return undefined;
    const token = {};
    escapeStack.push(token);
    const onKey = event => {
      if (event.key !== 'Escape' || escapeStack[escapeStack.length - 1] !== token) return;
      event.stopPropagation();
      handler.current?.(event);
    };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      const at = escapeStack.indexOf(token);
      if (at >= 0) escapeStack.splice(at, 1);
    };
  }, [active]);
}

// Calls onOutside when a pointer goes down outside every ref'd element.
export function useOutside(refs, active, onOutside) {
  const handler = useRef(onOutside);
  handler.current = onOutside;
  useEffect(() => {
    if (!active) return undefined;
    const onDown = event => {
      if (refs.some(ref => ref.current?.contains(event.target))) return;
      handler.current?.(event);
    };
    document.addEventListener('pointerdown', onDown);
    return () => document.removeEventListener('pointerdown', onDown);
  }, [active, refs]);
}

// Locks page scroll while a modal is open (nested modals share one lock).
let scrollLocks = 0;
export function useScrollLock(active) {
  useEffect(() => {
    if (!active) return undefined;
    scrollLocks += 1;
    if (scrollLocks === 1) document.documentElement.classList.add('rs-scroll-locked');
    return () => {
      scrollLocks -= 1;
      if (scrollLocks === 0) document.documentElement.classList.remove('rs-scroll-locked');
    };
  }, [active]);
}

// Places a floating element under (or above, when there's no room) its anchor.
export function useAnchoredPosition(anchorRef, floatingRef, open, { align = 'start', sideOffset = 6 } = {}) {
  const [style, setStyle] = useState({ position: 'fixed', top: 0, left: 0, visibility: 'hidden' });
  useLayoutEffect(() => {
    if (!open) return undefined;
    const place = () => {
      const anchor = anchorRef.current?.getBoundingClientRect?.();
      const floating = floatingRef.current;
      if (!anchor || !floating) return;
      const width = floating.offsetWidth;
      const height = floating.offsetHeight;
      const vw = window.innerWidth || document.documentElement.clientWidth;
      const vh = window.innerHeight || document.documentElement.clientHeight;
      let left = align === 'end' ? anchor.right - width : align === 'center' ? anchor.left + (anchor.width - width) / 2 : anchor.left;
      left = Math.max(8, Math.min(left, vw - width - 8));
      let top = anchor.bottom + sideOffset;
      if (top + height > vh - 8 && anchor.top - sideOffset - height > 8) top = anchor.top - sideOffset - height;
      setStyle({ position: 'fixed', top: Math.round(top), left: Math.round(left), minWidth: Math.min(anchor.width, vw - 16) });
    };
    place();
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
  }, [open, align, sideOffset, anchorRef, floatingRef]);
  return style;
}

// Arrow / Home / End movement between items that match `selector` inside a container.
export function moveFocus(container, selector, event) {
  const items = [...container.querySelectorAll(selector)].filter(el => !el.disabled && el.getAttribute('aria-disabled') !== 'true');
  if (!items.length) return false;
  const at = items.indexOf(document.activeElement);
  let next = null;
  if (event.key === 'ArrowDown' || event.key === 'ArrowRight') next = items[(at + 1) % items.length];
  else if (event.key === 'ArrowUp' || event.key === 'ArrowLeft') next = items[(at - 1 + items.length) % items.length];
  else if (event.key === 'Home') next = items[0];
  else if (event.key === 'End') next = items[items.length - 1];
  if (!next) return false;
  event.preventDefault();
  next.focus();
  return true;
}
