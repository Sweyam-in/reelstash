import React, { useCallback, useEffect, useRef, useState } from 'react';
import { getSpriteInfo, requestSprite } from '../api';
import { clampLabel, fractionAt, formatClock, markerNear, spriteTile } from '../playerUtils';

const SPRITE_POLL_MS = 3000;
const SPRITE_POLL_LIMIT = 40; // ~2 minutes of waiting for a sheet to be built

// The scrub-preview sheet for a title. If it does not exist yet the server is asked to build it,
// and this keeps checking until it is ready (or gives up quietly: the bar works without it).
function useSprite(mediaId, enabled) {
  const [sprite, setSprite] = useState(null);
  useEffect(() => {
    setSprite(null);
    if (!enabled) return undefined;
    let cancelled = false;
    let timer;
    let tries = 0;
    let asked = false;
    const check = async () => {
      try {
        const info = await getSpriteInfo(mediaId);
        if (cancelled) return;
        if (info.ready) {
          const img = new Image();
          img.src = info.url; // warm the cache so the first hover is instant
          setSprite(info);
          return;
        }
        if (!info.generating && !asked) { asked = true; await requestSprite(mediaId); }
      } catch { return; /* preview is optional; stop trying */ }
      tries += 1;
      if (!cancelled && tries < SPRITE_POLL_LIMIT) timer = setTimeout(check, SPRITE_POLL_MS);
    };
    check();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [mediaId, enabled]);
  return sprite;
}

/**
 * A seek bar under the video: click or drag to seek, hover to preview the frame at that
 * moment, and ticks for the person's markers. The native controls stay as they are.
 */
export default function PlayerTimeline({ videoRef, mediaId, markers, enabled = true, onSeek, bare = false }) {
  const barRef = useRef(null);
  const dragging = useRef(false);
  const dragTarget = useRef(null);
  const [clock, setClock] = useState({ current: 0, duration: 0 });
  const [previewTime, setPreviewTime] = useState(null);
  const [hover, setHover] = useState(null); // { fraction, x }
  const sprite = useSprite(mediaId, enabled);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const sync = () => setClock({ current: video.currentTime || 0, duration: Number.isFinite(video.duration) ? video.duration : 0 });
    const events = ['timeupdate', 'durationchange', 'loadedmetadata', 'seeked', 'emptied'];
    events.forEach(name => video.addEventListener(name, sync));
    sync();
    return () => events.forEach(name => video.removeEventListener(name, sync));
  }, [videoRef, mediaId, enabled]);

  const seekTo = useCallback(seconds => {
    const video = videoRef.current;
    if (!video || !clock.duration) return;
    const target = Math.min(clock.duration, Math.max(0, seconds));
    video.currentTime = target;
    setClock(value => ({ ...value, current: target }));
    onSeek?.(target);
  }, [videoRef, clock.duration, onSeek]);

  const seekFromPointer = event => {
    const fraction = fractionAt(event.clientX, barRef.current?.getBoundingClientRect());
    const target = fraction * clock.duration;
    dragTarget.current = target;
    setPreviewTime(target);
  };

  const onPointerMove = event => {
    const rect = barRef.current?.getBoundingClientRect();
    if (!rect || !clock.duration) return;
    setHover({ fraction: fractionAt(event.clientX, rect), x: event.clientX - rect.left });
    if (dragging.current) seekFromPointer(event);
  };
  const onPointerMoveRef = useRef(onPointerMove);
  onPointerMoveRef.current = onPointerMove;
  const stopDragListeners = useRef(null);
  const endDrag = event => {
    const target = dragTarget.current;
    dragging.current = false;
    dragTarget.current = null;
    setPreviewTime(null);
    stopDragListeners.current?.();
    stopDragListeners.current = null;
    if (event?.type !== 'pointercancel' && target != null) seekTo(target);
  };
  // Dragged via window-level listeners rather than setPointerCapture: WebKit on iOS/iPadOS can
  // silently stop delivering captured pointer events once the player's frame is fullscreened,
  // which left the seek bar unresponsive to touch there. Plain window listeners aren't affected.
  const onPointerDown = event => {
    if (!clock.duration || event.button > 0 || event.pointerType === 'touch') return;
    dragging.current = true;
    seekFromPointer(event);
    const move = e => onPointerMoveRef.current(e);
    window.addEventListener('pointermove', move);
    window.addEventListener('pointerup', endDrag);
    window.addEventListener('pointercancel', endDrag);
    stopDragListeners.current = () => {
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', endDrag);
      window.removeEventListener('pointercancel', endDrag);
    };
  };
  const seekToRef = useRef(seekTo);
  seekToRef.current = seekTo;
  useEffect(() => () => stopDragListeners.current?.(), []);
  // Touch fallback: some iOS/iPadOS fullscreen states never deliver pointer events to the bar, but
  // still deliver touch events. Handled natively (non-passive) so the page cannot claim the gesture.
  useEffect(() => {
    const bar = barRef.current;
    if (!bar) return undefined;
    let touching = false;
    let touchTarget = null;
    const previewTouch = event => {
      const touch = event.touches[0] || event.changedTouches[0];
      const video = videoRef.current;
      const duration = video && Number.isFinite(video.duration) ? video.duration : 0;
      if (!touch || !duration) return;
      const fraction = fractionAt(touch.clientX, bar.getBoundingClientRect());
      touchTarget = fraction * duration;
      setPreviewTime(touchTarget);
    };
    const start = event => { touching = true; previewTouch(event); event.preventDefault(); event.stopPropagation(); };
    const move = event => { if (!touching) return; previewTouch(event); event.preventDefault(); };
    const end = event => {
      if (!touching) return;
      touching = false;
      const target = touchTarget;
      touchTarget = null;
      setPreviewTime(null);
      if (event.type !== 'touchcancel' && target != null) seekToRef.current(target);
      event.stopPropagation();
    };
    bar.addEventListener('touchstart', start, { passive: false });
    bar.addEventListener('touchmove', move, { passive: false });
    bar.addEventListener('touchend', end);
    bar.addEventListener('touchcancel', end);
    return () => {
      bar.removeEventListener('touchstart', start);
      bar.removeEventListener('touchmove', move);
      bar.removeEventListener('touchend', end);
      bar.removeEventListener('touchcancel', end);
    };
  }, [videoRef]);
  const onKeyDown = event => {
    const step = event.shiftKey ? 30 : 5;
    if (event.key === 'ArrowLeft') { seekTo(clock.current - step); event.preventDefault(); }
    else if (event.key === 'ArrowRight') { seekTo(clock.current + step); event.preventDefault(); }
    else if (event.key === 'Home') { seekTo(0); event.preventDefault(); }
    else if (event.key === 'End') { seekTo(clock.duration); event.preventDefault(); }
  };

  const barWidth = barRef.current?.getBoundingClientRect().width || 0;
  const hoverSeconds = hover ? hover.fraction * clock.duration : 0;
  const nearMarker = hover && markers?.length ? markerNear(markers, hoverSeconds, (clock.duration / Math.max(barWidth, 1)) * 8) : null;
  const tile = hover && sprite ? spriteTile(sprite, hoverSeconds) : null;
  const labelWidth = tile ? tile.width : 96;
  const shownTime = previewTime ?? clock.current;
  const played = clock.duration ? (shownTime / clock.duration) * 100 : 0;

  return <div className="rs-timeline">
    {!bare && <span className="rs-timeline-time">{formatClock(shownTime)}</span>}
    <div
      ref={barRef}
      className={`rs-timeline-bar${clock.duration ? '' : ' is-idle'}`}
      role="slider"
      tabIndex={0}
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(clock.duration)}
      aria-valuenow={Math.round(shownTime)}
      aria-valuetext={`${formatClock(shownTime)} of ${formatClock(clock.duration)}`}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onPointerLeave={() => { if (!dragging.current) setHover(null); }}
      onKeyDown={onKeyDown}
    >
      <span className="rs-timeline-track"><span className="rs-timeline-played" style={{ width: `${played}%` }} /></span>
      <span className="rs-timeline-knob" style={{ left: `${played}%` }} aria-hidden="true" />
      {clock.duration > 0 && markers?.map(marker => <button
        key={marker.id}
        type="button"
        className="rs-timeline-marker"
        style={{ left: `${Math.min(100, (marker.seconds / clock.duration) * 100)}%` }}
        title={`${marker.title} · ${formatClock(marker.seconds)}`}
        aria-label={`Jump to ${marker.title} at ${formatClock(marker.seconds)}`}
        onPointerDown={event => event.stopPropagation()}
        onClick={() => seekTo(marker.seconds)}
      />)}
      {hover && clock.duration > 0 && <span className="rs-timeline-hover" style={{ left: hover.x }} aria-hidden="true" />}
      {hover && clock.duration > 0 && <span className="rs-timeline-tip" style={{ left: clampLabel(hover.x, labelWidth, barWidth), width: labelWidth }} aria-hidden="true">
        {tile && <span className="rs-timeline-thumb" style={{ width: tile.width, height: tile.height, backgroundImage: `url(${sprite.url})`, backgroundPosition: `-${tile.x}px -${tile.y}px` }} />}
        {nearMarker && <span className="rs-timeline-tip-marker">{nearMarker.title}</span>}
        <span className="rs-timeline-tip-time">{formatClock(hoverSeconds)}</span>
      </span>}
    </div>
    {!bare && <span className="rs-timeline-time">{formatClock(clock.duration)}</span>}
  </div>;
}
