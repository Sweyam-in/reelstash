/**
 * Fullscreen and Picture-in-Picture across browsers.
 *  - Chrome, Edge, Firefox, desktop Safari 16.4+, iPadOS: the frame goes fullscreen (so our controls come along).
 *  - Older Safari: same, through the webkit-prefixed API.
 *  - iPhone Safari: only the <video> itself can go fullscreen (with the system's controls).
 */

export const getFullscreenElement = (doc = document) => doc.fullscreenElement || doc.webkitFullscreenElement || null;

export function canElementFullscreen(frame, doc = document) {
  const enabled = doc.fullscreenEnabled ?? doc.webkitFullscreenEnabled ?? true;
  return Boolean(frame && enabled && (frame.requestFullscreen || frame.webkitRequestFullscreen));
}

export function isFullscreen(frame, video, doc = document) {
  return getFullscreenElement(doc) === frame || Boolean(video?.webkitDisplayingFullscreen);
}

/** Enters or leaves fullscreen, using whichever API this browser has. */
export async function toggleFullscreen(frame, video, doc = document) {
  if (getFullscreenElement(doc)) {
    const exit = doc.exitFullscreen || doc.webkitExitFullscreen;
    try { await exit?.call(doc); } catch { /* already leaving */ }
    return;
  }
  if (video?.webkitDisplayingFullscreen) { video.webkitExitFullscreen?.(); return; }
  if (canElementFullscreen(frame, doc)) {
    const request = frame.requestFullscreen || frame.webkitRequestFullscreen;
    try { await request.call(frame); return; } catch { /* fall through to the video itself */ }
  }
  try { video?.webkitEnterFullscreen?.(); } catch { /* no fullscreen available */ }
}

/** Calls fn whenever fullscreen starts or ends, by any of the events browsers use. Returns a cleanup. */
export function onFullscreenChange(video, fn, doc = document) {
  const docEvents = ['fullscreenchange', 'webkitfullscreenchange'];
  const videoEvents = ['webkitbeginfullscreen', 'webkitendfullscreen'];
  docEvents.forEach(name => doc.addEventListener(name, fn));
  videoEvents.forEach(name => video?.addEventListener(name, fn));
  return () => {
    docEvents.forEach(name => doc.removeEventListener(name, fn));
    videoEvents.forEach(name => video?.removeEventListener(name, fn));
  };
}

/** True when the page can offer Picture-in-Picture for this video. */
export function canPictureInPicture(video, doc = document) {
  if (!video) return false;
  if (typeof video.webkitSetPresentationMode === 'function' && video.webkitSupportsPresentationMode?.('picture-in-picture') === true) return true;
  return Boolean(doc.pictureInPictureEnabled && !video.disablePictureInPicture);
}

// Tries the standard API and Safari's presentation-mode API in turn, and checks that the video really
// moved: Safari's call does not throw when it is ignored. Resolves to '' on success or a reason.
export async function togglePictureInPicture(video, doc = document) {
  const isPip = () => doc.pictureInPictureElement === video || video.webkitPresentationMode === 'picture-in-picture';
  const wasPip = isPip();
  const webkit = typeof video.webkitSetPresentationMode === 'function' && video.webkitSupportsPresentationMode?.('picture-in-picture') === true;
  const attempts = [];
  if (doc.pictureInPictureEnabled && video.requestPictureInPicture) {
    attempts.push(() => (doc.pictureInPictureElement === video ? doc.exitPictureInPicture() : video.requestPictureInPicture()));
  }
  if (webkit) attempts.push(() => video.webkitSetPresentationMode(wasPip ? 'inline' : 'picture-in-picture'));
  let reason = 'Picture-in-picture is not available for this video.';
  for (const attempt of attempts) {
    try {
      await attempt();
      await new Promise(resolve => setTimeout(resolve, 150));
      if (isPip() !== wasPip) return '';
    } catch (error) { reason = error?.message || reason; console.warn('Picture-in-picture failed', error); }
  }
  return reason;
}

/**
 * iOS and some embedded browsers ignore element.volume (it always reads 1), so a
 * volume slider would do nothing there. Probe once on a throwaway element.
 */
let volumeSettable;
export function canSetVolume(create = () => document.createElement('video')) {
  if (volumeSettable !== undefined) return volumeSettable;
  try {
    const probe = create();
    probe.volume = 0.5;
    volumeSettable = probe.volume === 0.5;
  } catch { volumeSettable = false; }
  return volumeSettable;
}
export const __resetVolumeProbeForTests = () => { volumeSettable = undefined; };
