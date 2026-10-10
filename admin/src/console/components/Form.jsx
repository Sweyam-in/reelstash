import React, { createContext, useContext, useEffect, useId, useRef, useState } from 'react';
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@spatika/react';
import { AlertTriangle } from 'lucide-react';
import { useConfirm } from './Confirm';

// Whether the form has been submitted once: field errors stay hidden until then.
const AttemptedContext = createContext(true);

/**
 * A labelled form in a dialog. Guards unsaved changes, shows a specific failure inline (and keeps
 * what the admin typed), and keeps Save disabled until something changed and the form is valid.
 * Inline validation appears after the first submit attempt; focus then moves to the first error.
 */
export function FormDialog({ open, onOpenChange, title, description, size = 'md', dirty = false, canSubmit = true, submitting = false, submitLabel, error, onSubmit, children }) {
  const confirm = useConfirm();
  const formRef = useRef(null);
  const [attempted, setAttempted] = useState(false);

  useEffect(() => { if (open) setAttempted(false); }, [open]);

  const requestClose = async () => {
    if (submitting) return;
    if (dirty) {
      const ok = await confirm({ title: 'Discard unsaved changes?', consequence: 'The changes in this form have not been saved and will be lost.', confirmLabel: 'Discard changes', tone: 'danger' });
      if (!ok) return;
    }
    onOpenChange(false);
  };

  const submit = event => {
    event.preventDefault();
    if (submitting) return;
    setAttempted(true);
    if (!canSubmit) {
      // Let the fields render their errors, then move focus to the first.
      requestAnimationFrame(() => formRef.current?.querySelector('[aria-invalid="true"]')?.focus());
      return;
    }
    onSubmit();
  };

  return <Dialog open={open} onOpenChange={next => (next ? onOpenChange(true) : requestClose())}>
    <DialogContent size={size} className="console-dialog">
      <form ref={formRef} className="console-form" noValidate data-attempted={attempted || undefined} onSubmit={submit}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {error && <div className="console-callout" data-tone="crit" role="alert">
          <AlertTriangle aria-hidden="true" />
          <div><p>{error}</p><p>What you typed is still here. Fix the problem and save again.</p></div>
        </div>}
        <AttemptedContext.Provider value={attempted}>{children}</AttemptedContext.Provider>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={requestClose} disabled={submitting}>Cancel</Button>
          <Button type="submit" loading={submitting} disabled={!dirty}>{submitLabel}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>;
}

/**
 * Label + control + hint + inline error, wired for screen readers. `error` should only be passed
 * once the form has been submitted once (see FormDialog's data-attempted) so a blank form isn't
 * red on open.
 */
export function Field({ label, hint, error: rawError, optional = false, span, children }) {
  const attempted = useContext(AttemptedContext);
  const error = attempted ? rawError : null;
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ') || undefined;
  return <div className="console-field" data-invalid={error ? true : undefined} data-span={span}>
    <label className="console-field__label" htmlFor={id}>{label}{optional && <small>Optional</small>}</label>
    {children({ id, 'aria-describedby': describedBy, 'aria-invalid': error ? true : undefined })}
    {hint && <p className="console-field__hint" id={hintId}>{hint}</p>}
    {error && <p className="console-field__error" id={errorId}>{error}</p>}
  </div>;
}

export function SettingsGroup({ title, description, aside, footer, children, danger = false }) {
  return <section className={danger ? 'settings-group console-danger' : 'settings-group'}>
    <div className="settings-group__head">
      <div><h3>{title}</h3>{description && <p>{description}</p>}</div>
      {aside && <div className="settings-group__aside">{aside}</div>}
    </div>
    <div className="settings-group__rows">{children}</div>
    {footer && <div className="settings-group__foot">{footer}</div>}
  </section>;
}

export function SettingsRow({ label, description, htmlFor, children }) {
  return <div className="settings-row">
    <div className="settings-row__copy">
      {htmlFor ? <label className="settings-row__label" htmlFor={htmlFor}>{label}</label> : <span className="settings-row__label">{label}</span>}
      {description && <p className="settings-row__desc">{description}</p>}
    </div>
    <div className="settings-row__control">{children}</div>
  </div>;
}

export function SettingsEntity({ title, meta, aside }) {
  return <div className="settings-entity">
    <div className="settings-entity__copy"><span className="settings-entity__title">{title}</span>{meta && <span className="settings-entity__meta">{meta}</span>}</div>
    {aside && <div className="settings-entity__aside">{aside}</div>}
  </div>;
}
