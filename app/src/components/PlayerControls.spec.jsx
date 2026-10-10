import React from 'react';
import { describe, it, expect, vi } from 'vitest';
import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('./PlayerTimeline', () => ({ default: () => null }));
vi.mock('../fullscreen', () => ({ canPictureInPicture: () => false, canSetVolume: () => true, togglePictureInPicture: vi.fn() }));
import PlayerControls from './PlayerControls';

function setup(props = {}) {
  const video = props.video || document.createElement('video');
  const frame = document.createElement('div');
  const trackInfo = {
    audio: [{ index: 0, label: 'Japanese · AAC Stereo' }, { index: 1, label: 'English · AAC Stereo' }],
    audioIndex: 0, onAudio: vi.fn(),
    subtitles: [{ id: 'e0', label: 'English' }, { id: 'x1', label: 'Spanish' }],
    subtitleId: 'e0', onSubtitle: vi.fn(),
  };
  render(<PlayerControls videoRef={{ current: video }} frameRef={{ current: frame }} mediaId={1} markers={[]} onToggleFullscreen={vi.fn()} trackInfo={trackInfo} {...props} />);
  return { trackInfo, video };
}
const openMenu = () => userEvent.click(screen.getByRole('button', { name: 'Playback settings' }));

describe('PlayerControls track menu', () => {
  it('lists the server’s audio and subtitle tracks with the current ones checked', async () => {
    setup();
    await openMenu();
    expect(screen.getByRole('menuitemradio', { name: 'English', checked: true })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Spanish', checked: false })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Off', checked: false })).toBeInTheDocument();
    expect(screen.getByRole('menuitemradio', { name: 'Japanese · AAC Stereo', checked: true })).toBeInTheDocument();
  });

  it('switches subtitles, turns them off, and changes audio', async () => {
    const { trackInfo } = setup();
    await openMenu();
    await userEvent.click(screen.getByRole('menuitemradio', { name: 'Spanish' }));
    expect(trackInfo.onSubtitle).toHaveBeenCalledWith('x1');
    await userEvent.click(screen.getByRole('menuitemradio', { name: 'Off' }));
    expect(trackInfo.onSubtitle).toHaveBeenCalledWith(null);
    await userEvent.click(screen.getByRole('menuitemradio', { name: 'English · AAC Stereo' }));
    expect(trackInfo.onAudio).toHaveBeenCalledWith(1);
  });

  it('hides the audio choice when there is only one track', async () => {
    setup({ trackInfo: { audio: [{ index: 0, label: 'English' }], audioIndex: 0, onAudio: vi.fn(), subtitles: [], subtitleId: null, onSubtitle: vi.fn() } });
    await openMenu();
    expect(screen.queryByText('Audio')).not.toBeInTheDocument();
    expect(screen.queryByText('Subtitles')).not.toBeInTheDocument();
  });

  it('labels the next button for the episode case', () => {
    setup({ onNext: vi.fn(), nextLabel: 'Next episode' });
    expect(screen.getByRole('button', { name: 'Next episode' })).toBeInTheDocument();
  });
});

describe('PlayerControls loading overlay', () => {
  // jsdom videos never load: drive paused/readyState by hand and fire the media events.
  const fakeVideo = () => {
    const video = document.createElement('video');
    const state = { paused: true, readyState: 0 };
    Object.defineProperty(video, 'paused', { get: () => state.paused });
    Object.defineProperty(video, 'readyState', { get: () => state.readyState });
    const fire = (name, change = {}) => act(() => { Object.assign(state, change); video.dispatchEvent(new Event(name)); });
    return { video, fire };
  };
  const overlay = () => screen.queryByRole('status', { name: 'Loading video' });

  it('shows while play waits for the first frames and hides once playing', () => {
    const { video, fire } = fakeVideo();
    setup({ video });
    expect(overlay()).toBeNull();
    fire('play', { paused: false });
    expect(overlay()).toBeInTheDocument();
    fire('playing', { readyState: 4 });
    expect(overlay()).toBeNull();
  });

  it('shows on a stall mid-play and hides on pause', () => {
    const { video, fire } = fakeVideo();
    setup({ video });
    fire('play', { paused: false, readyState: 4 });
    expect(overlay()).toBeNull();
    fire('waiting', { readyState: 2 });
    expect(overlay()).toBeInTheDocument();
    fire('pause', { paused: true });
    expect(overlay()).toBeNull();
  });
});
