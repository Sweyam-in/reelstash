import React from 'react';
import { DialogDescription, DialogTitle } from '@spatika/react';
import { ExternalLink } from 'lucide-react';
import { mediaHref } from '../appLinks';

/**
 * The heading of a person, studio, series or video editor: the record's name, what it is, a few facts
 * and a link to its page in the media library (a new tab, so the editor stays open).
 */
export function EditorHeading({ kind, name, facts = [], libraryPath }) {
  const shown = facts.filter(Boolean);
  return <div className="rs-editor-heading">
    <DialogTitle>{name || `Loading ${kind.toLowerCase()}…`}</DialogTitle>
    <DialogDescription>
      <span>{kind}</span>
      {shown.map(fact => <span key={fact}>{fact}</span>)}
      {name && libraryPath && <a href={mediaHref(libraryPath)} target="_blank" rel="noopener">Open in library <ExternalLink size={12} aria-hidden="true" /></a>}
    </DialogDescription>
  </div>;
}

export const titleCount = n => `${(n || 0).toLocaleString()} ${n === 1 ? 'title' : 'titles'}`;
