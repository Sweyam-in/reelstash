import React from 'react';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@spatika/react';
import { Copy } from 'lucide-react';
import { initials } from '../lib/format';
import { useNotify } from '../lib/notify';

const AVATAR_HUES = [18, 32, 145, 168, 200, 222, 262, 292, 330];

function hueFor(seed) {
  let hash = 0;
  for (let i = 0; i < seed.length; i++) hash = (hash * 31 + seed.charCodeAt(i)) | 0;
  return AVATAR_HUES[Math.abs(hash) % AVATAR_HUES.length];
}

/** Coloured-initials avatar. The colour is stable per person and never carries meaning. */
export function UserAvatar({ name, email, size = 32 }) {
  return <span className="console-avatar" aria-hidden="true" style={{ width: size, height: size, fontSize: Math.max(10, Math.round(size * 0.36)), '--avatar-h': hueFor(email || name || '?') }}>
    {initials(name, email)}
  </span>;
}

/** Monospace value that copies itself on click. */
export function CopyButton({ value, label, display }) {
  const notify = useNotify();
  const copy = () => {
    navigator.clipboard.writeText(String(value))
      .then(() => notify.success(`${label} copied`))
      .catch(() => notify.info(`Couldn't copy the ${label.toLowerCase()}`, 'Your browser blocked clipboard access. Select the text and copy it by hand.'));
  };
  return <button type="button" className="console-copy" onClick={copy} aria-label={`Copy ${label}`} title={`Copy ${label}`}>
    <span>{display ?? value}</span><Copy size={12} aria-hidden="true" />
  </button>;
}

/** items: [{ term, details, key? }] */
export function DetailList({ items, flush = false }) {
  return <dl className={flush ? 'console-detail-list console-detail-list--flush' : 'console-detail-list'}>
    {items.filter(Boolean).map((item, i) => <div key={item.key ?? (typeof item.term === 'string' ? item.term : i)}><dt>{item.term}</dt><dd>{item.details}</dd></div>)}
  </dl>;
}

/** Right-side record inspector. A solid panel, never glass. */
export function DetailDrawer({ open, onClose, eyebrow, title, description, footer, children }) {
  return <Sheet open={open} onOpenChange={next => { if (!next) onClose(); }}>
    <SheetContent side="right" className="console-drawer">
      <SheetHeader className="console-drawer__head">
        {eyebrow && <p className="console-drawer__eyebrow">{eyebrow}</p>}
        <SheetTitle>{title}</SheetTitle>
        {description && <SheetDescription>{description}</SheetDescription>}
      </SheetHeader>
      <div className="console-drawer__body">{children}</div>
      {footer && <div className="console-drawer__foot">{footer}</div>}
    </SheetContent>
  </Sheet>;
}

export function DrawerSection({ title, children }) {
  return <section className="console-drawer__section"><h3>{title}</h3>{children}</section>;
}

export function Callout({ tone, icon, title, children }) {
  return <div className="console-callout" data-tone={tone} role={tone === 'crit' ? 'alert' : undefined}>
    {icon}
    <div><p><strong>{title}</strong></p>{children && <p>{children}</p>}</div>
  </div>;
}

/** Identity header of a detail page: avatar/icon, name, status, meta line, and the actions. */
export function ProfileHeader({ mark, title, status, meta, actions }) {
  return <header className="console-profile">
    <div className="console-profile__identity">
      {mark}
      <div>
        <h1 className="console-profile__name">{title}{status && <span className="console-profile__status">{status}</span>}</h1>
        {meta && <div className="console-profile__meta">{meta}</div>}
      </div>
    </div>
    {actions && <div className="console-profile__actions">{actions}</div>}
  </header>;
}
