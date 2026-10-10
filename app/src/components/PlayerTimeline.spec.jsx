import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({ getSpriteInfo: vi.fn(), requestSprite: vi.fn() }));
import { getSpriteInfo, requestSprite } from '../api';
import PlayerTimeline from './PlayerTimeline';

// A real <video> element whose clock we control.
function fakeVideo({ duration = 200, currentTime = 0 } = {}) {
  const video = document.createElement('video');
  let time = currentTime;
  Object.defineProperty(video, 'duration', { get: () => duration, configurable: true });
  Object.defineProperty(video, 'currentTime', { get: () => time, set: v => { time = v; }, configurable: true });
  return video;
}

function setup(props = {}) {
  const video = props.video || fakeVideo();
  const videoRef = { current: video };
  const onSeek = vi.fn();
  const utils = render(<PlayerTimeline videoRef={videoRef} mediaId={5} markers={[]} enabled={false} onSeek={onSeek} {...props.rest} />);
  act(() => { video.dispatchEvent(new Event('loadedmetadata')); });
  return { video, onSeek, ...utils };
}
const slider = () => screen.getByRole('slider', { name: 'Seek' });

beforeEach(() => {
  vi.clearAllMocks();
  getSpriteInfo.mockResolvedValue({ ready: false, generating: true });
  requestSprite.mockResolvedValue({});
});
afterEach(() => vi.useRealTimers());

describe('PlayerTimeline', () => {
  it('reports the video position and length on the slider', () => {
    setup({ video: fakeVideo({ duration: 3725, currentTime: 83 }) });
    expect(slider()).toHaveAttribute('aria-valuemax', '3725');
    expect(slider()).toHaveAttribute('aria-valuenow', '83');
    expect(slider()).toHaveAttribute('aria-valuetext', '1:23 of 1:02:05');
  });

  it('follows the video as it plays', () => {
    const { video } = setup();
    video.currentTime = 42;
    act(() => { video.dispatchEvent(new Event('timeupdate')); });
    expect(slider()).toHaveAttribute('aria-valuenow', '42');
  });

  it('is idle until the duration is known', () => {
    setup({ video: fakeVideo({ duration: NaN }) });
    expect(slider()).toHaveClass('is-idle');
    expect(slider()).toHaveAttribute('aria-valuemax', '0');
  });

  it('seeks with the arrow keys (5s) and Shift+arrow (30s)', async () => {
    const { video, onSeek } = setup({ video: fakeVideo({ currentTime: 60 }) });
    slider().focus();
    await userEvent.keyboard('{ArrowRight}');
    expect(video.currentTime).toBe(65);
    await userEvent.keyboard('{Shift>}{ArrowRight}{/Shift}');
    expect(video.currentTime).toBe(95);
    await userEvent.keyboard('{ArrowLeft}');
    expect(video.currentTime).toBe(90);
    expect(onSeek).toHaveBeenLastCalledWith(90);
  });

  it('jumps to the start and end with Home and End', async () => {
    const { video } = setup({ video: fakeVideo({ currentTime: 60 }) });
    slider().focus();
    await userEvent.keyboard('{End}');
    expect(video.currentTime).toBe(200);
    await userEvent.keyboard('{Home}');
    expect(video.currentTime).toBe(0);
  });

  it('never seeks outside the video', async () => {
    const { video } = setup({ video: fakeVideo({ currentTime: 2 }) });
    slider().focus();
    await userEvent.keyboard('{ArrowLeft}');
    expect(video.currentTime).toBe(0);
    video.currentTime = 198;
    act(() => { video.dispatchEvent(new Event('timeupdate')); });
    await userEvent.keyboard('{ArrowRight}');
    expect(video.currentTime).toBe(200);
  });

  it('ignores seeking before the duration is known', async () => {
    const { video, onSeek } = setup({ video: fakeVideo({ duration: NaN }) });
    slider().focus();
    await userEvent.keyboard('{ArrowRight}');
    expect(onSeek).not.toHaveBeenCalled();
    expect(video.currentTime).toBe(0);
  });

  it('draws a jump-to button for each marker and seeks when clicked', async () => {
    const markers = [{ id: 1, seconds: 50, title: 'Intro' }, { id: 2, seconds: 150, title: 'Finale' }];
    const { video, onSeek } = setup({ rest: { markers } });
    const button = screen.getByRole('button', { name: 'Jump to Finale at 2:30' });
    expect(button).toHaveStyle({ left: '75%' });
    expect(screen.getByRole('button', { name: 'Jump to Intro at 0:50' })).toHaveStyle({ left: '25%' });
    await userEvent.click(button);
    expect(video.currentTime).toBe(150);
    expect(onSeek).toHaveBeenCalledWith(150);
  });

  it('hides marker ticks until there is a duration to place them on', () => {
    setup({ video: fakeVideo({ duration: NaN }), rest: { markers: [{ id: 1, seconds: 5, title: 'X' }] } });
    expect(screen.queryByRole('button', { name: /Jump to X/ })).not.toBeInTheDocument();
  });

  it('shows the time under the pointer while hovering', () => {
    setup();
    const bar = slider();
    bar.getBoundingClientRect = () => ({ left: 0, width: 400, top: 0, height: 10, right: 400, bottom: 10 });
    fireEvent.pointerMove(bar, { clientX: 100 });
    expect(screen.getByText('0:50')).toBeInTheDocument(); // 100/400 of 200s
    fireEvent.pointerLeave(bar);
    expect(screen.queryByText('0:50')).not.toBeInTheDocument();
  });

  it('seeks to the pointer position on click-and-release', () => {
    const { video, onSeek } = setup();
    const bar = slider();
    bar.getBoundingClientRect = () => ({ left: 0, width: 400, top: 0, height: 10, right: 400, bottom: 10 });
    fireEvent.pointerDown(bar, { clientX: 300, button: 0, pointerType: 'mouse' });
    fireEvent.pointerUp(window, { clientX: 300 });
    expect(video.currentTime).toBe(150);
    expect(onSeek).toHaveBeenCalledWith(150);
  });

  it('a cancelled drag does not seek', () => {
    const { video } = setup();
    const bar = slider();
    bar.getBoundingClientRect = () => ({ left: 0, width: 400, top: 0, height: 10, right: 400, bottom: 10 });
    fireEvent.pointerDown(bar, { clientX: 300, button: 0, pointerType: 'mouse' });
    fireEvent.pointerCancel(window);
    expect(video.currentTime).toBe(0);
  });

  it('shows the total length', () => {
    setup({ video: fakeVideo({ duration: 3725 }) });
    expect(screen.getByText('1:02:05')).toBeInTheDocument();
  });
});

describe('PlayerTimeline — scrub previews', () => {
  it('does not ask for a sprite sheet when previews are off', async () => {
    setup();
    await new Promise(r => setTimeout(r, 20));
    expect(getSpriteInfo).not.toHaveBeenCalled();
  });

  it('asks the server to build one once, when none exists', async () => {
    getSpriteInfo.mockResolvedValue({ ready: false, generating: false });
    setup({ rest: { enabled: true } });
    await waitFor(() => expect(requestSprite).toHaveBeenCalledWith(5));
    expect(requestSprite).toHaveBeenCalledTimes(1);
  });

  it('does not re-request while one is being generated', async () => {
    setup({ rest: { enabled: true } });
    await waitFor(() => expect(getSpriteInfo).toHaveBeenCalled());
    expect(requestSprite).not.toHaveBeenCalled();
  });

  it('shows the frame under the pointer once the sheet is ready', async () => {
    getSpriteInfo.mockResolvedValue({ ready: true, url: '/sprite.jpg', intervalSeconds: 10, tileWidth: 160, tileHeight: 90, columns: 5, count: 20 });
    setup({ rest: { enabled: true } });
    await waitFor(() => expect(getSpriteInfo).toHaveBeenCalled());
    const bar = slider();
    bar.getBoundingClientRect = () => ({ left: 0, width: 400, top: 0, height: 10, right: 400, bottom: 10 });
    fireEvent.pointerMove(bar, { clientX: 300 }); // 150s -> tile 15 -> column 0, row 3
    await waitFor(() => expect(document.querySelector('.rs-timeline-thumb')).not.toBeNull());
    const thumb = document.querySelector('.rs-timeline-thumb');
    expect(thumb).toHaveStyle({ width: '160px', height: '90px', backgroundPosition: '-0px -270px' });
  });

  it('quietly gives up when the preview endpoint fails', async () => {
    getSpriteInfo.mockRejectedValue(new Error('nope'));
    setup({ rest: { enabled: true } });
    await waitFor(() => expect(getSpriteInfo).toHaveBeenCalledTimes(1));
    expect(slider()).toBeInTheDocument();
  });

  it('keeps polling until the sheet is ready', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    getSpriteInfo.mockResolvedValueOnce({ ready: false, generating: true }).mockResolvedValue({ ready: true, url: '/s.jpg', intervalSeconds: 10, tileWidth: 1, tileHeight: 1, columns: 1, count: 1 });
    setup({ rest: { enabled: true } });
    await waitFor(() => expect(getSpriteInfo).toHaveBeenCalledTimes(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(3100); });
    expect(getSpriteInfo).toHaveBeenCalledTimes(2);
    await act(async () => { await vi.advanceTimersByTimeAsync(10000); });
    expect(getSpriteInfo).toHaveBeenCalledTimes(2); // ready: stops
  });
});
