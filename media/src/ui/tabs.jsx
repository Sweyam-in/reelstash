import React, { createContext, useContext, useId, useState } from 'react';
import { cx, moveFocus } from './overlay';

// Accessible tabs (roving focus, arrow keys, Home/End). Controlled or uncontrolled.

const TabsContext = createContext(null);

export function Tabs({ value, defaultValue, onValueChange, className, children, orientation = 'horizontal', ...rest }) {
  const [inner, setInner] = useState(defaultValue);
  const current = value !== undefined ? value : inner;
  const base = useId();
  const select = next => { if (value === undefined) setInner(next); onValueChange?.(next); };
  return <TabsContext.Provider value={{ current, select, base, orientation }}>
    <div className={cx('rs-tabs', className)} data-orientation={orientation} {...rest}>{children}</div>
  </TabsContext.Provider>;
}

export function TabsList({ className, children, ...rest }) {
  const { orientation } = useContext(TabsContext);
  const onKeyDown = event => {
    if (moveFocus(event.currentTarget, '[role="tab"]', event)) document.activeElement?.click();
  };
  return <div role="tablist" aria-orientation={orientation} className={cx('rs-tabs-list', className)} onKeyDown={onKeyDown} {...rest}>{children}</div>;
}

const safe = value => String(value).replace(/[^a-zA-Z0-9_-]/g, '_');

export function TabsTrigger({ value, className, children, disabled, ...rest }) {
  const { current, select, base } = useContext(TabsContext);
  const selected = current === value;
  return <button type="button" role="tab" id={`${base}-tab-${safe(value)}`} aria-selected={selected} aria-controls={`${base}-panel-${safe(value)}`}
    tabIndex={selected ? 0 : -1} disabled={disabled} data-state={selected ? 'active' : 'inactive'}
    className={cx('rs-tab', selected && 'is-active', className)} onClick={() => select(value)} {...rest}>{children}</button>;
}

export function TabsContent({ value, className, children, forceMount = false, ...rest }) {
  const { current, base } = useContext(TabsContext);
  const selected = current === value;
  if (!selected && !forceMount) return null;
  return <div role="tabpanel" id={`${base}-panel-${safe(value)}`} aria-labelledby={`${base}-tab-${safe(value)}`} hidden={!selected} tabIndex={0}
    className={cx('rs-tab-panel', className)} {...rest}>{children}</div>;
}
