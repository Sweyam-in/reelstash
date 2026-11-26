import React, { cloneElement, createContext, isValidElement, useContext, useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Check } from 'lucide-react';
import { useTheme } from './basics';
import { cx, moveFocus, useAnchoredPosition, useEscape, useOutside } from './overlay';

// Dropdown menus and popovers anchored to a trigger. Both portal to <body> and are positioned
// beside the trigger; menus get roving focus with arrow keys, popovers keep normal Tab order.

const Ctx = createContext(null);

function useDisclosure(open, onOpenChange) {
  const [inner, setInner] = useState(false);
  const isOpen = open !== undefined ? open : inner;
  const setOpen = next => { if (open === undefined) setInner(next); onOpenChange?.(next); };
  return [isOpen, setOpen];
}

function Root({ open, onOpenChange, kind, children }) {
  const [isOpen, setOpen] = useDisclosure(open, onOpenChange);
  const triggerRef = useRef(null);
  const contentRef = useRef(null);
  const id = useId();
  const value = useMemo(() => ({ open: isOpen, setOpen, triggerRef, contentRef, id, kind }), [isOpen, id, kind]); // eslint-disable-line react-hooks/exhaustive-deps
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const DropdownMenu = props => <Root kind="menu" {...props} />;
export const Popover = props => <Root kind="popover" {...props} />;

function Trigger({ asChild = false, children, className, onClick, ...rest }) {
  const { open, setOpen, triggerRef, id, kind } = useContext(Ctx);
  const props = {
    ref: triggerRef,
    'aria-haspopup': kind === 'menu' ? 'menu' : 'dialog',
    'aria-expanded': open,
    'aria-controls': open ? `${id}-content` : undefined,
    onClick: event => { onClick?.(event); if (!event.defaultPrevented) setOpen(!open); },
    onKeyDown: event => { if (kind === 'menu' && event.key === 'ArrowDown' && !open) { event.preventDefault(); setOpen(true); } },
    ...rest,
  };
  if (asChild && isValidElement(children)) {
    const child = children;
    return cloneElement(child, { ...props, className: cx(className, child.props.className), onClick: event => { child.props.onClick?.(event); props.onClick(event); } });
  }
  return <button type="button" className={className} {...props}>{children}</button>;
}

export const DropdownMenuTrigger = Trigger;
export const PopoverTrigger = Trigger;

function Content({ align = 'start', sideOffset = 6, className, children, style, onKeyDown, ...rest }) {
  const { open, setOpen, triggerRef, contentRef, id, kind } = useContext(Ctx);
  const theme = useTheme();
  const position = useAnchoredPosition(triggerRef, contentRef, open, { align, sideOffset });
  const refs = useMemo(() => [triggerRef, contentRef], [triggerRef, contentRef]);
  useOutside(refs, open, () => setOpen(false));
  useEscape(open, () => { setOpen(false); triggerRef.current?.focus?.(); });
  useEffect(() => {
    if (!open) return;
    const root = contentRef.current;
    if (!root) return;
    const first = kind === 'menu'
      ? root.querySelector('[role^="menuitem"][aria-checked="true"], [role^="menuitem"]')
      : root.querySelector('[data-autofocus], input, button, [href], [tabindex]:not([tabindex="-1"])');
    first?.focus({ preventScroll: true });
  }, [open, kind, contentRef]);
  if (!open || typeof document === 'undefined') return null;
  const keys = event => {
    onKeyDown?.(event);
    if (kind === 'menu') moveFocus(event.currentTarget, '[role^="menuitem"]', event);
    if (kind === 'menu' && event.key === 'Tab') setOpen(false);
  };
  return createPortal(<div className="rs-theme rs-float-layer" data-rs-theme={theme}>
    <div ref={contentRef} id={`${id}-content`} role={kind === 'menu' ? 'menu' : 'dialog'}
      className={cx(kind === 'menu' ? 'rs-menu' : 'rs-popover', className)} style={{ ...position, ...style }} onKeyDown={keys} {...rest}>{children}</div>
  </div>, document.body);
}

export const DropdownMenuContent = Content;
export const PopoverContent = Content;

function useClose() { return useContext(Ctx).setOpen; }

function select(handler, close) {
  return event => {
    handler?.(event);
    if (!event.defaultPrevented) { close(false); }
  };
}

export function DropdownMenuItem({ onSelect, className, children, disabled, inset, ...rest }) {
  const close = useClose();
  return <button type="button" role="menuitem" tabIndex={-1} disabled={disabled} aria-disabled={disabled || undefined}
    className={cx('rs-menu-item', inset && 'is-inset', className)} onClick={select(onSelect, close)} {...rest}>{children}</button>;
}

export function DropdownMenuCheckboxItem({ checked, onCheckedChange, onSelect, className, children, ...rest }) {
  const close = useClose();
  return <button type="button" role="menuitemcheckbox" aria-checked={!!checked} tabIndex={-1} className={cx('rs-menu-item is-check', className)}
    onClick={select(event => { onSelect?.(event); onCheckedChange?.(!checked); }, close)} {...rest}>
    <span className="rs-menu-indicator" aria-hidden="true">{checked && <Check size={15} />}</span>{children}
  </button>;
}

const RadioCtx = createContext(null);
export function DropdownMenuRadioGroup({ value, onValueChange, children, ...rest }) {
  return <RadioCtx.Provider value={{ value, onValueChange }}><div role="group" {...rest}>{children}</div></RadioCtx.Provider>;
}

export function DropdownMenuRadioItem({ value, onSelect, className, children, ...rest }) {
  const close = useClose();
  const group = useContext(RadioCtx) || {};
  const checked = group.value === value;
  return <button type="button" role="menuitemradio" aria-checked={checked} tabIndex={-1} className={cx('rs-menu-item is-check', className)}
    onClick={select(event => { onSelect?.(event); group.onValueChange?.(value); }, close)} {...rest}>
    <span className="rs-menu-indicator" aria-hidden="true">{checked && <Check size={15} />}</span>{children}
  </button>;
}

export const DropdownMenuLabel = ({ className, ...rest }) => <div className={cx('rs-menu-label', className)} {...rest} />;
export const DropdownMenuSeparator = ({ className }) => <div role="separator" className={cx('rs-separator', className)} />;
export const PopoverClose = ({ children, ...rest }) => { const close = useClose(); return <button type="button" onClick={() => close(false)} {...rest}>{children}</button>; };
