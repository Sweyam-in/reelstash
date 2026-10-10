import React, { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger, Input } from '@spatika/react';
import { Bookmark, Trash2 } from 'lucide-react';
import { useFilterPresets } from '../lib/useFilterPresets';
import { useNotify } from '../lib/notify';
import { Field, FormDialog } from './Form';

/** Named filter views for a list page, kept in this browser. A view is the page's URL query. */
export function SavedViews({ storageKey }) {
  const [params, setParams] = useSearchParams();
  const { presets, savePreset, deletePreset } = useFilterPresets(storageKey);
  const notify = useNotify();
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState('');
  const current = params.toString();
  return <>
    <DropdownMenu>
      <DropdownMenuTrigger asChild><Button type="button" variant="outline" size="sm" leadingIcon={<Bookmark size={14} />}>Views</Button></DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Saved views</DropdownMenuLabel>
        {presets.length === 0 ? <DropdownMenuItem disabled>No saved views yet</DropdownMenuItem> : presets.map(preset => <DropdownMenuItem key={preset.name} onSelect={() => setParams(new URLSearchParams(preset.filters), { replace: true })}>
          <span className="console-truncate">{preset.name}</span>
          <button type="button" className="console-copy" aria-label={`Delete view ${preset.name}`} onClick={event => { event.stopPropagation(); deletePreset(preset.name); }}><Trash2 size={14} aria-hidden="true" /></button>
        </DropdownMenuItem>)}
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={!current} onSelect={() => { setName(''); setNaming(true); }}>Save current filters…</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    <FormDialog open={naming} onOpenChange={setNaming} title="Save view" description="Saves the current search, filters and sort in this browser." size="sm" submitLabel="Save view" dirty={name.length > 0} canSubmit={name.trim().length > 0}
      onSubmit={() => { savePreset(name.trim(), current); setNaming(false); notify.success(`View “${name.trim()}” saved`); }}>
      <Field label="Name" error={name.trim() ? null : 'Give the view a name.'}>{props => <Input {...props} autoFocus value={name} maxLength={40} onChange={e => setName(e.target.value)} placeholder="Videos without subtitles" />}</Field>
    </FormDialog>
  </>;
}
