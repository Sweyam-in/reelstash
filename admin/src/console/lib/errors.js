// Turn API failures into messages that say what happened and what to do next.

export const isForbiddenError = error => !!error && typeof error === 'object' && error.status === 403;
export const isLockedError = error => !!error && typeof error === 'object' && error.status === 423;

export function errorMessage(error) {
  if (!error) return '';
  if (error instanceof Error) return error.message;
  return String(error);
}

const NETWORK_RE = /failed to fetch|networkerror|load failed|network request failed|unknown network error/i;

// "Couldn't start the scan. <reason>." Never a bare "Something went wrong".
export function describeError(action, error) {
  const raw = errorMessage(error).trim();
  if (isForbiddenError(error)) return `You don't have permission to ${action}. Only administrators can change this.`;
  if (isLockedError(error)) return `Couldn't ${action} because ReelStash is locked. Unlock it with your PIN and try again.`;
  if (NETWORK_RE.test(raw)) return `Couldn't ${action}. The server is unreachable. Check the connection or System status, then try again.`;
  if (!raw) return `Couldn't ${action}. Try again, and check System status if it keeps failing.`;
  const sentence = raw.charAt(0).toUpperCase() + raw.slice(1);
  return `Couldn't ${action}. ${sentence}${/[.!?]$/.test(sentence) ? '' : '.'}`;
}
