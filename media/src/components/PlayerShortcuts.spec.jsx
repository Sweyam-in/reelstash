import React from 'react';
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import PlayerShortcuts, { PLAYER_SHORTCUTS } from './PlayerShortcuts';

describe('PlayerShortcuts', () => {
  it('lists every shortcut with its keys when open', () => {
    render(<PlayerShortcuts open onOpenChange={() => {}} />);
    expect(screen.getByText('Keyboard shortcuts')).toBeInTheDocument();
    for (const [, rows] of PLAYER_SHORTCUTS) {
      for (const [, label] of rows) expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(screen.getByText('Volume up or down')).toBeInTheDocument();
  });

  it('renders nothing while closed', () => {
    render(<PlayerShortcuts open={false} onOpenChange={() => {}} />);
    expect(screen.queryByText('Keyboard shortcuts')).not.toBeInTheDocument();
  });
});
