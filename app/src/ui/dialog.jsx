import React, { createContext, useContext, useId, useRef } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { useTheme } from './basics';
import { cx, useEscape, useFocusTrap, useScrollLock } from './overlay';

// Modal dialogs: a centred panel on desktop that becomes a bottom sheet on phones.

const DialogContext = createContext(null);
const useDialog = () => useContext(DialogContext) || {};

function Root({ open = false, onOpenChange, alert = false, children }) {
  const titleId = useId();
  const descriptionId = useId();
  const close = () => onOpenChange?.(false);
  return <DialogContext.Provider value={{ open, close, titleId, descriptionId, alert }}>{children}</DialogContext.Provider>;
}

export const Dialog = props => <Root {...props} />;
export const AlertDialog = props => <Root alert {...props} />;

function Content({ size = 'md', className, children, showClose, onEscapeKeyDown, onPointerDownOutside, ...rest }) {
  const { open, close, titleId, descriptionId, alert } = useDialog();
  const theme = useTheme();
  const panel = useRef(null);
  useFocusTrap(panel, open);
  useScrollLock(open);
  useEscape(open, event => { onEscapeKeyDown?.(event); if (!event.defaultPrevented) close(); });
  if (!open || typeof document === 'undefined') return null;
  const onOverlay = event => {
    if (event.target !== event.currentTarget || alert) return;
    onPointerDownOutside?.(event);
    if (!event.defaultPrevented) close();
  };
  return createPortal(<div className="rs-theme rs-dialog-layer" data-rs-theme={theme}>
    <div className="rs-dialog-overlay" onMouseDown={onOverlay}>
      <div ref={panel} role={alert ? 'alertdialog' : 'dialog'} aria-modal="true" aria-labelledby={titleId} aria-describedby={rest['aria-describedby'] ?? descriptionId} tabIndex={-1}
        className={cx('rs-dialog', `rs-dialog--${size}`, className)} {...rest}>
        {(showClose ?? !alert) && <button type="button" className="rs-dialog-close" aria-label="Close" onClick={close}><X size={18} /></button>}
        {children}
      </div>
    </div>
  </div>, document.body);
}

export const DialogContent = Content;
export const AlertDialogContent = props => <Content size="sm" {...props} />;

export const DialogHeader = ({ className, ...rest }) => <div className={cx('rs-dialog-header', className)} {...rest} />;
export const DialogBody = ({ className, ...rest }) => <div className={cx('rs-dialog-body', className)} {...rest} />;
export const DialogFooter = ({ className, ...rest }) => <div className={cx('rs-dialog-footer', className)} {...rest} />;
export const AlertDialogHeader = DialogHeader;
export const AlertDialogFooter = DialogFooter;

export function DialogTitle({ className, ...rest }) {
  const { titleId } = useDialog();
  return <h2 id={titleId} className={cx('rs-dialog-title', className)} {...rest} />;
}
export const AlertDialogTitle = DialogTitle;

export function DialogDescription({ className, ...rest }) {
  const { descriptionId } = useDialog();
  return <p id={descriptionId} className={cx('rs-dialog-description', className)} {...rest} />;
}
export const AlertDialogDescription = DialogDescription;

export function AlertDialogCancel({ className, children = 'Cancel', onClick, ...rest }) {
  const { close } = useDialog();
  return <button type="button" className={cx('rs-btn rs-btn--secondary rs-btn--md', className)} onClick={event => { onClick?.(event); if (!event.defaultPrevented) close(); }} {...rest}>{children}</button>;
}

export function AlertDialogAction({ className, variant = 'danger', ...rest }) {
  return <button type="button" className={cx('rs-btn', `rs-btn--${variant === 'destructive' ? 'danger' : variant}`, 'rs-btn--md', className)} {...rest} />;
}

export const DialogClose = ({ children, ...rest }) => {
  const { close } = useDialog();
  return <button type="button" onClick={close} {...rest}>{children}</button>;
};
