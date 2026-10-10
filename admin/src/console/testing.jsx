import React from 'react';
import { renderApp } from '../testing/render';
import { ConfirmProvider } from './components/Confirm';
import { SessionProvider } from './lib/session';

/** Renders a console page inside the providers it expects: router, toasts, confirm dialogs and a session. */
export function renderConsole(ui, { route = '/admin', session = { id: 1, isAdmin: true, displayName: 'Admin' }, ...options } = {}) {
  return renderApp(<SessionProvider session={session}><ConfirmProvider>{ui}</ConfirmProvider></SessionProvider>, { route, ...options });
}
