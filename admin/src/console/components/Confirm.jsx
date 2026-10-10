import React, { createContext, useCallback, useContext, useId, useRef, useState } from 'react';
import { AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Input } from '@spatika/react';

// One confirmation pattern for anything that changes production or is hard to undo. Every dialog
// states what will happen, who or what is affected and whether it can be undone; it can ask for a
// reason when the backend records one, and require typed confirmation for irreversible actions.
//
//   const confirm = useConfirm();
//   const result = await confirm({ title, consequence, affects, reversible: true, confirmLabel: 'Disable' });
//   if (result) await save(result.reason);
//
// options: { title, consequence, affects?, reversible?: boolean | string, confirmLabel, tone?: 'danger',
//            reason?: { label?, placeholder?, required? }, typeToConfirm?: string }

const ConfirmContext = createContext(null);

export function useConfirm() {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error('useConfirm must be used inside <ConfirmProvider>');
  return ctx;
}

export function ConfirmProvider({ children }) {
  const [options, setOptions] = useState(null);
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState('');
  const [typed, setTyped] = useState('');
  const resolver = useRef(null);
  const reasonId = useId();
  const typedId = useId();

  const confirm = useCallback(next => {
    resolver.current?.(null);
    setOptions(next);
    setReason('');
    setTyped('');
    setOpen(true);
    return new Promise(resolve => { resolver.current = resolve; });
  }, []);

  const settle = value => {
    setOpen(false);
    resolver.current?.(value);
    resolver.current = null;
  };

  const reasonRequired = !!options?.reason && options.reason.required !== false;
  const typedOk = !options?.typeToConfirm || typed.trim().toLowerCase() === options.typeToConfirm.trim().toLowerCase();
  const reasonOk = !reasonRequired || reason.trim().length > 0;
  const canConfirm = typedOk && reasonOk;
  const reversibleText = options?.reversible === undefined ? null
    : typeof options.reversible === 'string' ? options.reversible
      : options.reversible ? 'Yes, this can be undone later.' : 'No. This is permanent and cannot be undone.';

  return <ConfirmContext.Provider value={confirm}>
    {children}
    <AlertDialog open={open} onOpenChange={next => { if (!next) settle(null); }}>
      <AlertDialogContent className="console-dialog">
        {options && <form className="console-form" onSubmit={event => { event.preventDefault(); if (canConfirm) settle({ reason: reason.trim() }); }}>
          <AlertDialogHeader>
            <AlertDialogTitle>{options.title}</AlertDialogTitle>
            <AlertDialogDescription>{options.consequence}</AlertDialogDescription>
          </AlertDialogHeader>
          {(options.affects || reversibleText) && <dl className="console-confirm__facts">
            {options.affects && <div><dt>Affects</dt><dd>{options.affects}</dd></div>}
            {reversibleText && <div><dt>Reversible</dt><dd style={options.reversible === false ? { color: 'var(--console-crit)', fontWeight: 600 } : undefined}>{reversibleText}</dd></div>}
          </dl>}
          {options.reason && <div className="console-field">
            <label className="console-field__label" htmlFor={reasonId}>{options.reason.label ?? 'Reason'}{!reasonRequired && <small>Optional</small>}</label>
            <Input id={reasonId} autoFocus value={reason} onChange={e => setReason(e.target.value)} placeholder={options.reason.placeholder ?? 'Why are you doing this?'} maxLength={500} />
            <p className="console-field__hint">Recorded in the audit log.</p>
          </div>}
          {options.typeToConfirm && <div className="console-field">
            <label className="console-field__label" htmlFor={typedId}>Type <span className="console-code">{options.typeToConfirm}</span> to confirm</label>
            <Input id={typedId} autoFocus={!options.reason} value={typed} onChange={e => setTyped(e.target.value)} autoComplete="off" spellCheck={false} aria-invalid={typed.length > 0 && !typedOk ? true : undefined} />
          </div>}
          <AlertDialogFooter>
            <Button type="button" variant="outline" onClick={() => settle(null)}>Cancel</Button>
            <Button type="submit" variant={options.tone === 'danger' ? 'destructive' : 'default'} disabled={!canConfirm}>{options.confirmLabel}</Button>
          </AlertDialogFooter>
        </form>}
      </AlertDialogContent>
    </AlertDialog>
  </ConfirmContext.Provider>;
}
