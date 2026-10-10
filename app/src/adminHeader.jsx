import React, { createContext, useContext, useEffect, useState } from 'react';
import { createPortal } from 'react-dom';

// Structured admin page header. The app shell's top bar carries the page's bold title, the library
// scope (on pages that can narrow by library) and the page's main actions. Below it, the page intro
// is one row: the description on the left, secondary actions on the right. Pages declare all of it
// with <PageHeader>, <PageActions>, <FloatingActions> and <LibraryScope>; the shell supplies the slots.
const HeaderContext = createContext(null);

export function AdminHeaderProvider({ children }) {
  const [title, setTitle] = useState(null);
  const [slot, setSlot] = useState(null);
  const [floatSlot, setFloatSlot] = useState(null);
  const [scopeSlot, setScopeSlot] = useState(null);
  const value = React.useMemo(() => ({ title, setTitle, slot, setSlot, floatSlot, setFloatSlot, scopeSlot, setScopeSlot }), [title, slot, floatSlot, scopeSlot]);
  return <HeaderContext.Provider value={value}>{children}</HeaderContext.Provider>;
}

// Rendered inside the TopBar: the page title (falls back to the nav label), then the scope slot.
export function AdminHeaderTitle({ fallback }) {
  const header = useContext(HeaderContext);
  return <span className="rs-header-heading">
    <strong className="rs-header-title">{header?.title || fallback}</strong>
    <span className="rs-header-scope" ref={header?.setScopeSlot} />
  </span>;
}
export function AdminHeaderSlot() {
  const header = useContext(HeaderContext);
  return <div className="rs-header-page-actions" ref={header?.setSlot} />;
}

export function PageActions({ children }) {
  const header = useContext(HeaderContext);
  if (!children) return null;
  if (!header) return <div className="rs-header-page-actions">{children}</div>; // no shell (e.g. a page rendered alone)
  if (!header.slot) return null;
  return createPortal(children, header.slot);
}

// Puts its children next to the page title in the top bar (the library scope switcher).
export function HeaderScope({ children }) {
  const header = useContext(HeaderContext);
  if (!children) return null;
  if (!header) return children;
  if (!header.scopeSlot) return null;
  return createPortal(children, header.scopeSlot);
}

// Secondary page actions: they sit at the right end of the intro row, in the page flow. Several
// <FloatingActions> on one page join the same group (the name is kept from when the group floated).
export function FloatingActions({ children }) {
  const header = useContext(HeaderContext);
  if (!children) return null;
  if (!header) return <div className="rs-floating-actions-inline">{children}</div>;
  if (!header.floatSlot) return null;
  return createPortal(children, header.floatSlot);
}

// Drop-in for Spatika's PageHeader: title and `actions` go to the shell top bar; the description and
// `secondary` actions share the intro row at the top of the page.
export function PageHeader({ title, description, actions, secondary, meta, children }) {
  const header = useContext(HeaderContext);
  const setTitle = header?.setTitle;
  useEffect(() => {
    if (!setTitle) return undefined;
    setTitle(title);
    return () => setTitle(null);
  }, [setTitle, title]);
  return <>
    <PageActions>{actions}</PageActions>
    <div className="rs-page-intro">
      <div className="rs-page-intro-row">
        {(description || meta) ? <p>{description}{meta}</p> : <span />}
        {header ? <div className="rs-page-tools" role="toolbar" aria-label="More actions" ref={header.setFloatSlot} /> : null}
      </div>
      {children}
    </div>
    <FloatingActions>{secondary}</FloatingActions>
  </>;
}
