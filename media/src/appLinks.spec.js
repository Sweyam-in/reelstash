import { describe, it, expect, vi, beforeEach } from 'vitest';

// appLinks keeps the partner origins in module state, so each test gets a fresh copy.
async function freshModule(config) {
  vi.resetModules();
  const fetchMock = vi.fn().mockImplementation(async () => {
    if (config instanceof Error) throw config;
    return { ok: true, status: 200, json: async () => config };
  });
  vi.stubGlobal('fetch', fetchMock);
  const mod = await import('./appLinks.js');
  return { ...mod, fetchMock };
}

describe('appLinks', () => {
  beforeEach(() => vi.unstubAllGlobals());

  it('keeps links same-origin until the server has said where the other app lives', async () => {
    const { adminHref, mediaHref } = await freshModule({});
    expect(adminHref('/admin/jobs')).toBe('/admin/jobs');
    expect(mediaHref('/library')).toBe('/library');
  });

  it('points cross-app links at the partner origin once loaded', async () => {
    const { adminHref, mediaHref, loadAppUrls } = await freshModule({ mediaUrl: 'https://media.example', adminUrl: 'https://admin.example' });
    await loadAppUrls();
    expect(adminHref('/admin/jobs')).toBe('https://admin.example/admin/jobs');
    expect(mediaHref('/library/3')).toBe('https://media.example/library/3');
  });

  it('asks the server only once however many callers load it', async () => {
    const { loadAppUrls, fetchMock } = await freshModule({ mediaUrl: 'https://m' });
    await Promise.all([loadAppUrls(), loadAppUrls()]);
    await loadAppUrls();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe('/auth/config');
  });

  it('falls back to same-origin links when the config cannot be loaded', async () => {
    const { adminHref, loadAppUrls } = await freshModule(new Error('offline'));
    await expect(loadAppUrls()).resolves.toBeUndefined();
    expect(adminHref('/x')).toBe('/x');
  });
});
