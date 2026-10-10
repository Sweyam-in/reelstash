import '@testing-library/jest-dom/vitest';
import { afterEach, vi } from 'vitest';
import { cleanup } from '@testing-library/react';

afterEach(() => {
  cleanup();
  try { localStorage.clear(); sessionStorage.clear(); } catch { /* ignore */ }
});

// jsdom lacks these; components that measure or observe layout expect them.
globalThis.ResizeObserver ||= class { observe() {} unobserve() {} disconnect() {} };
globalThis.IntersectionObserver ||= class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
window.matchMedia ||= query => ({
  matches: false, media: query, onchange: null,
  addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent: () => false,
});
Element.prototype.scrollIntoView ||= vi.fn();
// Radix (menus, selects, dialogs) probes pointer capture, which jsdom lacks.
Element.prototype.hasPointerCapture ||= () => false;
Element.prototype.setPointerCapture ||= () => {};
Element.prototype.releasePointerCapture ||= () => {};
window.scrollTo = vi.fn();
