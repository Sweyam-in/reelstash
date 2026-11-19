import { userLabel } from './format.js';

// Account facts shared by the users list, the detail page and the safety checks.
export const isAdminUser = user => user.role === 'ADMIN';

export function userStatus(user) {
  if (user.disabled) return { tone: 'crit', label: 'Disabled' };
  if (!user.lastLoginAt) return { tone: 'neutral', label: 'Never signed in' };
  return { tone: 'ok', label: 'Active' };
}

export const signInMethod = user => (user.passwordLogin ? 'Username and password' : 'Single sign-on');
export const accountName = userLabel;

// The backend refuses to demote or disable your own account and the last enabled administrator (and
// to delete either); the console says so up front instead of letting the request fail.
// enabledAdmins is how many enabled administrators exist, from the server's own count.
export function accountGuard(user, enabledAdmins, sessionId) {
  const lastAdmin = isAdminUser(user) && !user.disabled && enabledAdmins <= 1;
  const lastAdminText = 'This is the only enabled administrator. Make someone else an administrator first.';
  const isSelf = user.id === sessionId;
  return {
    isSelf,
    lastAdmin,
    cannotDisable: isSelf ? 'You can’t disable the account you are signed in with.' : lastAdmin ? lastAdminText : null,
    cannotDemote: isSelf ? 'You can’t remove your own administrator role.' : lastAdmin ? lastAdminText : null,
    cannotDelete: isSelf ? 'You can’t delete the account you are signed in with.' : lastAdmin ? lastAdminText : null,
  };
}
