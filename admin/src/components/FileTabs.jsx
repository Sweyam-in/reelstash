import React from 'react';
import { Folder, HardDrive, Plus, X } from 'lucide-react';
import { tabLabel } from '../filesTabs';

// The browser-style tab strip above the file list. Each tab keeps its own folder and history.
export default function FileTabs({ tabs, activeId, locations, onActivate, onClose, onNew }) {
  return <div className="rs-fm-tabs" role="tablist" aria-label="Open folders">
    {tabs.map(tab => {
      const label = tabLabel(tab.url, locations);
      const atRoot = /^\/files\/[^/]+\/?$/.test(tab.url);
      return <div key={tab.id} role="tab" tabIndex={tab.id === activeId ? 0 : -1} aria-selected={tab.id === activeId} className={`rs-fm-tab ${tab.id === activeId ? 'is-active' : ''}`} title={label}
        onClick={() => onActivate(tab.id)}
        onAuxClick={event => { if (event.button === 1) { event.preventDefault(); onClose(tab.id); } }}
        onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onActivate(tab.id); } }}>
        {atRoot ? <HardDrive size={14} /> : <Folder size={14} />}
        <span className="rs-fm-tab-label">{label}</span>
        {tabs.length > 1 && <button type="button" className="rs-fm-tab-close" tabIndex={-1} aria-label={`Close tab ${label}`} onClick={event => { event.stopPropagation(); onClose(tab.id); }}><X size={12} /></button>}
      </div>;
    })}
    <button type="button" className="rs-fm-tab-new" aria-label="New tab" title="New tab (Alt+T)" onClick={onNew}><Plus size={15} /></button>
  </div>;
}
