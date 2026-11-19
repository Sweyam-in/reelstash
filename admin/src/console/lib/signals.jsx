import React, { createContext, useContext, useEffect, useMemo } from 'react';
import { useLocation } from 'react-router-dom';
import { getHealth, getMatchSuggestions } from '../../api';
import { useResource } from './useResource';

// Signals the shell and the dashboard share, loaded once: system health (for the top-bar pill,
// the System page and "needs attention") and the review-queue size (sidebar badge). Polled
// slowly and refreshed when the admin navigates, at most every 30 seconds. /admin/health is a
// read with no audit trail, so this is cheap.

const SignalsContext = createContext(null);
const REFRESH_MS = 60_000;

// Maps a health response to the pill: tone ok | warn | crit, a label and a count of problems.
export function healthSummary(health) {
  if (!health) return { tone: 'neutral', label: 'Checking…', issues: 0 };
  const checks = health.checks || [];
  const errors = checks.filter(c => c.status === 'error').length;
  const warnings = checks.filter(c => c.status === 'warning').length;
  if (errors) return { tone: 'crit', label: errors === 1 ? '1 problem' : `${errors} problems`, issues: errors + warnings };
  if (warnings) return { tone: 'warn', label: warnings === 1 ? '1 warning' : `${warnings} warnings`, issues: warnings };
  return { tone: 'ok', label: 'All systems normal', issues: 0 };
}

export function SignalsProvider({ children }) {
  const { pathname } = useLocation();
  const health = useResource(() => getHealth(), [], { interval: REFRESH_MS });
  const review = useResource(() => getMatchSuggestions({ limit: 1 }).then(r => r.total || 0), [], { interval: REFRESH_MS * 2 });
  const { reload: reloadHealth } = health;
  const { reload: reloadReview } = review;

  // Refresh on navigation, but not more often than every 30 seconds.
  const last = React.useRef(Date.now());
  useEffect(() => {
    if (Date.now() - last.current < 30_000) return;
    last.current = Date.now();
    reloadHealth();
    reloadReview();
  }, [pathname, reloadHealth, reloadReview]);

  const value = useMemo(() => ({
    health: health.data, healthError: health.error, healthLoading: health.loading, reloadHealth,
    reviewCount: review.data ?? 0,
    summary: healthSummary(health.data),
  }), [health.data, health.error, health.loading, reloadHealth, review.data]);
  return <SignalsContext.Provider value={value}>{children}</SignalsContext.Provider>;
}

// Outside a provider (the restricted view for non-admins) there are no signals and no requests.
const NO_SIGNALS = { health: null, healthError: null, healthLoading: false, reloadHealth: () => {}, reviewCount: 0, summary: healthSummary(null) };

export function useSignals() {
  return useContext(SignalsContext) || NO_SIGNALS;
}
