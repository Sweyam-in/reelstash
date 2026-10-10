import React, { Suspense, lazy, useEffect } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { SpatikaThemeProvider, Toaster, TooltipProvider } from '@spatika/react';
import { Lock } from 'lucide-react';
import { AppLockProvider } from './components/AppLock';
import { NotificationsProvider } from './components/Notifications';
import { AuthGate } from './shell';
import adminLogo from './assets/logo-admin.svg';
import { ConfirmProvider, EmptyState, PageStack, Panel, SkeletonRows } from './console/components';
import { ConsoleLayout } from './console/shell/ConsoleLayout';
import { SignalsProvider } from './console/lib/signals';
import { Button } from '@spatika/react';
import { mediaHref } from './appLinks';
import { SessionProvider } from './console/lib/session';

// ReelStash Admin: the admin console, an app of its own (admin.html) deployed beside ReelStash
// Files and Downloads. It talks to the same HTTP API as the media library and shares no source
// with it.

// Pages load on first visit so the dashboard isn't waiting on code it doesn't use.
const page = (loader, name) => lazy(() => loader().then(m => ({ default: m[name] || m.default })));
const Dashboard = page(() => import('./console/pages/DashboardPage'), 'DashboardPage');
const Jobs = page(() => import('./console/pages/JobsPage'), 'JobsPage');
const Users = page(() => import('./console/pages/UsersPage'), 'UsersPage');
const Libraries = page(() => import('./console/pages/LibrariesPage'), 'LibrariesPage');
const LibraryDetail = page(() => import('./console/pages/LibraryDetailPage'), 'LibraryDetailPage');
const Videos = page(() => import('./console/pages/VideosPage'), 'VideosPage');
const Storage = page(() => import('./console/pages/StoragePage'), 'StoragePage');
const DiskDetail = page(() => import('./console/pages/DiskDetailPage'), 'DiskDetailPage');
const ScanRules = page(() => import('./console/pages/ScanRulesPage'), 'ScanRulesPage');
const System = page(() => import('./console/pages/SystemPage'), 'SystemPage');
const Analytics = page(() => import('./console/pages/AnalyticsPage'), 'AnalyticsPage');
const Review = page(() => import('./console/pages/ReviewPage'), 'ReviewPage');
const Duplicates = page(() => import('./console/pages/DuplicatesPage'), 'DuplicatesPage');
const Automation = page(() => import('./console/pages/AutomationPage'), 'AutomationPage');
const Housekeeping = page(() => import('./console/pages/HousekeepingPage'), 'HousekeepingPage');
const Plugins = page(() => import('./console/pages/PluginsPage'), 'PluginsPage');
const Notifications = page(() => import('./console/pages/NotificationsPage'), 'NotificationsPage');
const Appearance = page(() => import('./console/pages/AppearancePage'), 'AppearancePage');
const Providers = page(() => import('./console/pages/ProvidersPage'), 'ProvidersPage');
const Organizer = page(() => import('./console/pages/OrganizerPage'), 'OrganizerPage');
const Rename = page(() => import('./console/pages/RenamePage'), 'RenamePage');
const People = page(() => import('./console/pages/MetadataPages'), 'PerformersPage');
const Studios = page(() => import('./console/pages/MetadataPages'), 'StudiosPage');
const Series = page(() => import('./console/pages/MetadataPages'), 'SeriesPage');
const Categories = page(() => import('./console/pages/MetadataPages'), 'CategoriesPage');
const Setup = page(() => import('./console/pages/SetupPage'), 'SetupPage');
const Audit = page(() => import('./console/pages/AuditPage'), 'AuditPage');
const UserDetail = page(() => import('./console/pages/UserDetailPage'), 'UserDetailPage');
function PageFallback() {
  return <PageStack><Panel bodyless><SkeletonRows rows={6} label="Loading page" /></Panel></PageStack>;
}

function Routed() {
  return <Suspense fallback={<PageFallback />}>
    <Routes>
      <Route path="/admin" element={<Dashboard />} />
      <Route path="/admin/jobs" element={<Jobs />} />
      <Route path="/admin/libraries" element={<Libraries />} />
      <Route path="/admin/libraries/:id" element={<LibraryDetail />} />
      <Route path="/admin/videos" element={<Videos />} />
      <Route path="/admin/system" element={<System />} />
      <Route path="/admin/analytics" element={<Analytics />} />
      <Route path="/admin/review" element={<Review />} />
      <Route path="/admin/duplicates" element={<Duplicates />} />
      <Route path="/admin/automation" element={<Automation />} />
      <Route path="/admin/housekeeping" element={<Housekeeping />} />
      <Route path="/admin/plugins" element={<Plugins />} />
      <Route path="/admin/notifications" element={<Notifications />} />
      <Route path="/admin/appearance" element={<Appearance />} />
      <Route path="/admin/metadata/providers" element={<Providers />} />
      <Route path="/admin/organizer" element={<Organizer />} />
      <Route path="/admin/rename" element={<Rename />} />
      <Route path="/admin/metadata" element={<Navigate to="/admin/metadata/performers" replace />} />
      <Route path="/admin/metadata/performers" element={<People />} />
      <Route path="/admin/metadata/studios" element={<Studios />} />
      <Route path="/admin/metadata/series" element={<Series />} />
      <Route path="/admin/metadata/categories" element={<Categories />} />
      <Route path="/admin/audit" element={<Audit />} />
      <Route path="/admin/users" element={<Users />} />
      <Route path="/admin/users/:id" element={<UserDetail />} />
      <Route path="/admin/setup" element={<Setup />} />
      <Route path="/admin/storage" element={<Storage />} />
      <Route path="/admin/storage/:id" element={<DiskDetail />} />
      <Route path="/admin/scan-rules" element={<ScanRules />} />
      <Route path="*" element={<Navigate to="/admin" replace />} />
    </Routes>
  </Suspense>;
}

// A signed-in non-admin sees an inline permission state inside the shell, never a splash, and no
// admin request is made for them.
function Restricted({ session, onSignOut }) {
  return <ConsoleLayout session={session} onSignOut={onSignOut} restricted>
    <PageStack><Panel bodyless>
      <EmptyState centered icon={Lock} title="The admin console is for administrators"
        message="Your account can use ReelStash and Files, but changing libraries, users and settings needs an administrator. Ask one to change your role."
        action={<><Button asChild><a href="/files">Open Files</a></Button><Button variant="outline" asChild><a href={mediaHref('/')}>Open ReelStash</a></Button></>} />
    </Panel></PageStack>
  </ConsoleLayout>;
}

function AdminAppContent({ session, onSignOut }) {
  const { pathname, search } = useLocation();
  // /admin/downloads and /admin/files are the sibling workspaces at /downloads and /files (another bundle).
  const sibling = pathname.match(/^\/admin(\/(?:downloads|files)(?:\/.*)?)$/)?.[1];
  useEffect(() => { if (sibling) window.location.replace(sibling + search); }, [sibling, search]);
  if (sibling) return null;
  if (pathname !== '/admin' && !pathname.startsWith('/admin/')) return <Navigate to="/admin" replace />;
  if (!session.isAdmin) return <Restricted session={session} onSignOut={onSignOut} />;
  return <SignalsProvider>
    <ConsoleLayout session={session} onSignOut={onSignOut}><Routed /></ConsoleLayout>
  </SignalsProvider>;
}

export default function AdminApp() {
  return <SpatikaThemeProvider defaultTheme="usha" storageKey="reelstash-admin-theme" className="contents">
    <Toaster position="bottom-right"><TooltipProvider><ConfirmProvider><BrowserRouter><AppLockProvider>
      <AuthGate appName="ReelStash Admin" logo={adminLogo}>{(session, signOut) =>
        <NotificationsProvider enabled={!!session.isAdmin}><SessionProvider session={session}><AdminAppContent session={session} onSignOut={signOut} /></SessionProvider></NotificationsProvider>}
      </AuthGate>
    </AppLockProvider></BrowserRouter></ConfirmProvider></TooltipProvider></Toaster>
  </SpatikaThemeProvider>;
}
