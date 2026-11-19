// Plain-language names for audit actions ("users.update" → "Changed a user"). Anything not listed
// falls back to the action itself, so a new admin route is still readable in the log.
const LABELS = {
  'users.create': 'Added a user', 'users.update': 'Changed a user’s role or status', 'users.delete': 'Deleted a user',
  'users.password': 'Reset a password', 'users.libraries': 'Changed library access', 'users.sessions.delete': 'Signed a user out',
  'auth.update': 'Changed sign-in methods', 'libraries.create': 'Created a library', 'libraries.update': 'Edited a library',
  'libraries.delete': 'Deleted a library', 'libraries.scan': 'Started a library scan', 'libraries.order': 'Reordered libraries', 'libraries.watch': 'Changed file watching',
  'storage.create': 'Added a storage location', 'storage.update': 'Edited a storage location', 'storage.delete': 'Deleted a storage location',
  'jobs.run': 'Started a job', 'jobs.cancel': 'Stopped a job', 'jobs.retry': 'Retried a job', 'jobs.finished.delete': 'Cleared finished jobs',
  'housekeeping.run': 'Ran housekeeping', 'housekeeping.settings': 'Changed housekeeping retention',
  'schedules.create': 'Added a schedule', 'schedules.update': 'Changed a schedule', 'schedules.delete': 'Removed a schedule',
  'webhooks.create': 'Added a webhook', 'webhooks.update': 'Changed a webhook', 'webhooks.delete': 'Removed a webhook',
  'plugins.install': 'Installed a plugin', 'plugins.enabled': 'Switched a plugin on or off', 'plugins.config': 'Changed plugin settings',
  'plugins.delete': 'Uninstalled a plugin', 'plugins.update': 'Updated a plugin', 'plugins.restart': 'Restarted a plugin',
  'providers.config': 'Changed provider settings', 'providers.order': 'Changed matching settings', 'providers.external.create': 'Connected a provider plugin', 'providers.external.delete': 'Removed a provider plugin',
  'appearance.update': 'Changed the library theme', 'scan-rules.create': 'Added a scan rule', 'scan-rules.delete': 'Removed a scan rule',
  'media.delete': 'Deleted a title', 'duplicates.scan': 'Started a duplicate scan', 'scans.create': 'Started a disk scan',
  'organizer.run': 'Organized files', 'organizer.cleanup': 'Cleaned up folders', 'rename.create': 'Renamed a file',
};

export const auditLabel = action => LABELS[action] || action;

// Filter groups for the log: [value (action prefix), label]
export const AUDIT_GROUPS = [
  ['', 'Everything'], ['users.', 'Users'], ['auth.', 'Sign-in'], ['libraries.', 'Libraries'], ['storage.', 'Storage'], ['jobs.', 'Jobs'],
  ['housekeeping.', 'Housekeeping'], ['plugins.', 'Plugins'], ['providers.', 'Providers'], ['webhooks.', 'Webhooks'], ['media.', 'Titles'],
];

// Where a target links to, when the console has a page for it.
export function auditTargetLink(event) {
  if (!event.targetId) return null;
  return { users: `/admin/users/${event.targetId}`, libraries: `/admin/libraries/${event.targetId}`, storage: `/admin/storage/${event.targetId}` }[event.targetKind] || null;
}
