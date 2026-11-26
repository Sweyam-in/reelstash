import React, { Children, cloneElement, createContext, forwardRef, isValidElement, useContext, useId } from 'react';
import { cx } from './overlay';

// ReelStash's own building blocks: theme scope, buttons, form fields and small status pieces.

export const THEME_DARK = 'dark';
export const THEME_LIGHT = 'light';

// Older builds stored Spatika theme names; map them onto the two ReelStash themes.
export const normalizeTheme = value => (value === 'light' || value === 'mukta' || value === 'usha' ? THEME_LIGHT : THEME_DARK);

const ThemeContext = createContext(THEME_DARK);
export const useTheme = () => useContext(ThemeContext);

// Scopes the colour tokens (ui/tokens.css) to a subtree. Portaled overlays read the theme from
// <body data-rs-theme>, which App sets for the media app.
export function ThemeScope({ theme = THEME_DARK, className, children, as: Tag = 'div', ...rest }) {
  const resolved = normalizeTheme(theme);
  return <ThemeContext.Provider value={resolved}>
    <Tag className={cx('rs-theme', className)} data-rs-theme={resolved} {...rest}>{children}</Tag>
  </ThemeContext.Provider>;
}

// Renders `children`'s only element with the merged props instead of a wrapper (asChild).
export function Slot({ children, className, ...props }) {
  const child = Children.only(children);
  if (!isValidElement(child)) return null;
  const merged = { ...props, ...child.props };
  for (const key of Object.keys(props)) {
    if (/^on[A-Z]/.test(key) && typeof props[key] === 'function' && typeof child.props[key] === 'function') {
      merged[key] = event => { child.props[key](event); if (!event?.defaultPrevented) props[key](event); };
    }
  }
  merged.className = cx(className, child.props.className);
  return cloneElement(child, merged);
}

const VARIANTS = { primary: 'primary', default: 'primary', secondary: 'secondary', outline: 'outline', ghost: 'ghost', danger: 'danger', destructive: 'danger', link: 'link' };

export const Button = forwardRef(function Button({ variant = 'primary', size = 'md', loading = false, asChild = false, className, children, disabled, type, ...rest }, ref) {
  const classes = cx('rs-btn', `rs-btn--${VARIANTS[variant] || 'primary'}`, `rs-btn--${size}`, loading && 'is-loading', className);
  if (asChild) return <Slot ref={ref} className={classes} {...rest}>{children}</Slot>;
  return <button ref={ref} type={type || 'button'} className={classes} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
    {loading && <span className="rs-btn-spinner" aria-hidden="true" />}
    {children}
  </button>;
});

export const Input = forwardRef(function Input({ className, ...rest }, ref) {
  return <input ref={ref} className={cx('rs-input', className)} {...rest} />;
});

export const Textarea = forwardRef(function Textarea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={cx('rs-input rs-textarea', className)} {...rest} />;
});

// A labelled form row. The label points at htmlFor, else at the single control inside (given an id).
export function FormField({ label, htmlFor, description, error, className, children, ...rest }) {
  const generated = useId();
  let controlId = htmlFor;
  let content = children;
  const only = Children.count(children) === 1 && isValidElement(children) ? children : null;
  if (!controlId && only) {
    controlId = only.props.id || `field-${generated}`;
    const describedBy = [description && `${controlId}-desc`, error && `${controlId}-err`].filter(Boolean).join(' ') || undefined;
    content = cloneElement(only, { id: controlId, 'aria-describedby': only.props['aria-describedby'] || describedBy, 'aria-invalid': error ? true : only.props['aria-invalid'] });
  }
  return <div className={cx('rs-field', className)} {...rest}>
    {label && <label className="rs-field-label" htmlFor={controlId}>{label}</label>}
    {content}
    {description && <p className="rs-field-hint" id={controlId ? `${controlId}-desc` : undefined}>{description}</p>}
    {error && <p className="rs-field-error" id={controlId ? `${controlId}-err` : undefined} role="alert">{error}</p>}
  </div>;
}

export function Badge({ variant = 'neutral', className, ...rest }) {
  return <span className={cx('rs-badge', `rs-badge--${variant}`, className)} {...rest} />;
}

export function Alert({ variant = 'info', className, children, ...rest }) {
  return <div role="alert" className={cx('rs-alert', `rs-alert--${variant}`, className)} {...rest}>{children}</div>;
}

export function Card({ className, as: Tag = 'div', ...rest }) {
  return <Tag className={cx('rs-card', className)} {...rest} />;
}

export function Spinner({ label = 'Loading', className, size = 20 }) {
  return <span role="status" aria-label={label} className={cx('rs-spinner', className)} style={{ width: size, height: size }} />;
}

export function EmptyState({ icon, title, description, action, className }) {
  return <div className={cx('rs-empty', className)}>
    {icon && <span className="rs-empty-icon" aria-hidden="true">{icon}</span>}
    {title && <h2 className="rs-empty-title">{title}</h2>}
    {description && <p className="rs-empty-text">{description}</p>}
    {action}
  </div>;
}

// A single-choice row of options, as radio buttons (keyboard: arrows move and select).
export function SegmentedControl({ options, value, onChange, className, name, ...rest }) {
  const group = useId();
  return <div role="radiogroup" className={cx('rs-segmented', className)} {...rest}>
    {options.map(option => {
      const id = `${group}-${option.value}`;
      const checked = option.value === value;
      return <label key={option.value} htmlFor={id} className={cx('rs-segmented-option', checked && 'is-checked', option.disabled && 'is-disabled')}>
        <input id={id} type="radio" name={name || group} value={option.value} checked={checked} disabled={option.disabled} onChange={() => onChange?.(option.value)} />
        <span>{option.label}</span>
      </label>;
    })}
  </div>;
}

// Small visual separator for menus and toolbars.
export const Separator = ({ className }) => <div role="separator" className={cx('rs-separator', className)} />;
