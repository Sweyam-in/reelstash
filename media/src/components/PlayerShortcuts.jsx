import React from 'react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../ui';

// Every key the player answers to (MediaDetail's onFrameKeyDown). Keep the two in step.
export const PLAYER_SHORTCUTS = [
  ['Playback', [
    [['Space', 'K'], 'Play or pause'],
    [['J'], 'Back 10 seconds'],
    [['L'], 'Forward 10 seconds'],
    [['←', '→'], 'Back or forward 5 seconds'],
    [['0–9'], 'Jump to 0%–90%'],
    [['Home', 'End'], 'Start or end'],
    [[',', '.'], 'Previous or next frame (while paused)'],
    [['<', '>'], 'Slower or faster'],
  ]],
  ['Sound and picture', [
    [['↑', '↓'], 'Volume up or down'],
    [['M'], 'Mute'],
    [['C'], 'Subtitles on or off'],
    [['F'], 'Full screen'],
  ]],
  ['Navigation', [
    [['[', ']'], 'Previous or next marker'],
    [['I'], 'Skip to the intro marker'],
    [['Shift+N', 'Shift+P'], 'Next or previous in queue'],
    [['?'], 'Show these shortcuts'],
  ]],
];

export default function PlayerShortcuts({ open, onOpenChange }) {
  return <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Keyboard shortcuts</DialogTitle>
        <DialogDescription>Click the video first so it has keyboard focus.</DialogDescription>
      </DialogHeader>
      <div className="rs-shortcuts">
        {PLAYER_SHORTCUTS.map(([group, rows]) => <section key={group}>
          <h3>{group}</h3>
          <dl>{rows.map(([keys, label]) => <div key={label}>
            <dt>{keys.map((key, index) => <React.Fragment key={key}>{index > 0 && <span className="rs-shortcuts-or">or</span>}<kbd>{key}</kbd></React.Fragment>)}</dt>
            <dd>{label}</dd>
          </div>)}</dl>
        </section>)}
      </div>
    </DialogContent>
  </Dialog>;
}
