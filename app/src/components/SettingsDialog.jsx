import React, { useEffect, useRef, useState } from 'react';
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button, Dialog, DialogBody, DialogContent, DialogFooter, Tabs, TabsList, TabsTrigger } from '../ui';

/**
 * The shell shared by the settings modals (title, person, studio, series): a header with a picture,
 * the kind of settings and the name; sections as tabs (a side list when `vertical`); and a footer.
 *
 *  - `tabs` is [{ value, label, icon?: Icon, dirty?: boolean }]; `status` sits beside the close button.
 *  - Saving: `onSave` runs from the footer button and from Ctrl/⌘+S; `canSave` and `saving` drive the
 *    button; `showSave={false}` hides it on tabs that save by themselves (the footer then says Close).
 *  - `dirty` makes closing ask before throwing edits away.
 *  - `note` is the small line at the left of the footer.
 */
export default function SettingsDialog({ open, onOpenChange, className = '', vertical = false, lead, kicker, title, status, loading, tabs, tab, onTab, dirty = false, onSave, canSave = true, saving = false, showSave = true, note, after, children }) {
  const [confirmClose, setConfirmClose] = useState(false);
  const saveRef = useRef(onSave);
  saveRef.current = canSave && !saving ? onSave : null;
  const requestOpenChange = next => { if (!next && dirty) setConfirmClose(true); else onOpenChange(next); };

  // Ctrl/⌘+S saves instead of the browser's "save page".
  useEffect(() => {
    if (!open) return undefined;
    const onKey = event => {
      if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === 's') {
        event.preventDefault();
        if (showSave) saveRef.current?.();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, showSave]);

  return <Dialog open={open} onOpenChange={requestOpenChange}>
    <DialogContent size="xl" className={`rs-pm${vertical ? ' is-vertical' : ''}${className ? ` ${className}` : ''}`}>
      <header className="rs-pm-head">
        {lead && <span className="rs-pm-lead">{lead}</span>}
        <div className="rs-pm-heading"><span>{kicker}</span><strong>{title}</strong></div>
        <span className="rs-pm-spacer" />
        {dirty && <span className="rs-pm-unsaved" role="status"><i />Unsaved changes</span>}
        {status}
      </header>
      <DialogBody className="rs-pm-body">
        {loading
          ? <p className="rs-pm-muted rs-pm-loading" data-autofocus tabIndex={-1}>Loading…</p>
          : <Tabs value={tab} onValueChange={onTab} orientation={vertical ? 'vertical' : 'horizontal'}>
            <TabsList aria-label="Sections">
              {tabs.map(({ value, label, icon: Icon, dirty: tabDirty }) => <TabsTrigger key={value} value={value} data-autofocus={value === tab ? '' : undefined}>
                {Icon && <Icon size={18} aria-hidden="true" />}
                <span className="rs-tab-label">{label}</span>
                {tabDirty && <span className="rs-pm-dirty" aria-label="Unsaved changes" />}
              </TabsTrigger>)}
            </TabsList>
            <div className="rs-pm-panel">{children}</div>
          </Tabs>}
      </DialogBody>
      {!loading && <DialogFooter className="rs-pm-foot">
        {note && <span className="rs-pm-note">{note}</span>}
        <span className="rs-pm-spacer" />
        <Button variant="secondary" onClick={() => requestOpenChange(false)}>{showSave && dirty ? 'Cancel' : showSave ? 'Cancel' : 'Close'}</Button>
        {showSave && <Button disabled={!canSave || saving} onClick={() => onSave?.()}>{saving ? 'Saving…' : 'Save changes'}</Button>}
      </DialogFooter>}
    </DialogContent>
    <AlertDialog open={confirmClose} onOpenChange={setConfirmClose}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
          <AlertDialogDescription>Your edits have not been saved.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Keep editing</AlertDialogCancel>
          <AlertDialogAction onClick={() => { setConfirmClose(false); onOpenChange(false); }}>Discard</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
    {after}
  </Dialog>;
}
