import React from 'react';
import { Button, Skeleton } from '@spatika/react';
import { AlertTriangle, Lock, RotateCw } from 'lucide-react';
import { errorMessage, isForbiddenError } from '../lib/errors';

/** Placeholder rows that match real list rows, so content doesn't jump on load. */
export function SkeletonRows({ rows = 4, label = 'Loading' }) {
  return <div className="console-skeleton-rows" role="status" aria-label={label}>
    {Array.from({ length: rows }, (_, i) => <div key={i} className="console-skeleton-rows__row">
      <Skeleton className="console-skeleton-rows__mark" />
      <span className="console-skeleton-rows__lines">
        <Skeleton className="console-skeleton-rows__line" style={{ width: `${62 - ((i * 13) % 30)}%` }} />
        <Skeleton className="console-skeleton-rows__line console-skeleton-rows__line--sub" />
      </span>
    </div>)}
  </div>;
}

/**
 * A named failure with a way out. Permission failures get their own wording and no retry;
 * everything else offers Retry.
 */
export function ErrorState({ title, error, onRetry, compact = false, centered = false, children }) {
  const denied = isForbiddenError(error);
  const Icon = denied ? Lock : AlertTriangle;
  const message = denied ? 'Only administrators can open this. Sign in with an administrator account to continue.' : errorMessage(error) || 'The server returned an error.';
  return <div className="console-state" data-tone={denied ? 'muted' : 'warn'} data-compact={compact || undefined} data-centered={centered || undefined} role="alert">
    <Icon className="console-state__icon" aria-hidden="true" />
    <div className="console-state__copy">
      <p className="console-state__title">{denied ? "You don't have permission to view this" : title}</p>
      <p className="console-state__message">{message}</p>
    </div>
    {(children || (!denied && onRetry)) && <div className="console-state__actions">
      {!denied && onRetry && <Button type="button" size="sm" variant="outline" onClick={onRetry} leadingIcon={<RotateCw size={14} />}>Retry</Button>}
      {children}
    </div>}
  </div>;
}

export function EmptyState({ icon: Icon, title, message, action, compact = false, centered = false }) {
  return <div className="console-state" data-tone="muted" data-compact={compact || undefined} data-centered={centered || undefined}>
    {Icon && <Icon className="console-state__icon" aria-hidden="true" />}
    <div className="console-state__copy">
      <p className="console-state__title">{title}</p>
      {message && <p className="console-state__message">{message}</p>}
    </div>
    {action && <div className="console-state__actions">{action}</div>}
  </div>;
}

/** Inline status: dot + text, never a filled pill and never colour alone. tone: ok | warn | crit | neutral | accent */
export function Status({ tone = 'neutral', children, pulse = false }) {
  return <span className="console-status" data-tone={tone} data-pulse={pulse || undefined}>
    <span className="console-status__dot" aria-hidden="true" />
    {children}
  </span>;
}

export function Tag({ children, tone, title }) {
  return <span className="console-tag" data-tone={tone} title={title}>{children}</span>;
}

/** Determinate bar with a text value next to it. percent null = indeterminate. */
export function ProgressBar({ percent, label = 'Progress', tone }) {
  return <div className={`console-progress${percent == null ? ' is-indeterminate' : ''}`} data-tone={tone} role="progressbar" aria-label={label}
    aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent ?? undefined}>
    <span style={percent == null ? undefined : { width: `${percent}%` }} />
  </div>;
}

/** Capacity meter: how full something is. Tone follows the fill, and the number is always printed beside it. */
export function Meter({ fraction, label }) {
  const percent = Math.round(fraction * 100);
  const tone = fraction >= 0.9 ? 'crit' : fraction >= 0.75 ? 'warn' : undefined;
  return <div className="console-progress" data-tone={tone} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}>
    <span style={{ width: `${percent}%` }} />
  </div>;
}
