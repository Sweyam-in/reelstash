import React from 'react';
import { Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@spatika/react';
import { MoreHorizontal } from 'lucide-react';

/** The "More" menu: secondary and destructive actions. items: [{ label, icon, onSelect, danger, disabled } | 'separator'] */
export function MoreMenu({ items, label = 'More', align = 'end', iconOnly = false }) {
  const visible = items.filter(Boolean);
  if (!visible.length) return null;
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      {iconOnly ? <Button type="button" variant="ghost" size="icon-sm" aria-label={label}><MoreHorizontal size={16} /></Button>
        : <Button type="button" variant="outline" size="sm" leadingIcon={<MoreHorizontal size={14} />}>{label}</Button>}
    </DropdownMenuTrigger>
    <DropdownMenuContent align={align}>
      {visible.map((item, i) => item === 'separator' ? <DropdownMenuSeparator key={`s${i}`} />
        : <DropdownMenuItem key={item.label} disabled={item.disabled} onSelect={item.onSelect} className={item.danger ? 'console-menu-danger' : undefined}>
          {item.icon && <item.icon size={14} aria-hidden="true" />}{item.label}
        </DropdownMenuItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}
