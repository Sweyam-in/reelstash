import React from 'react';
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SpatikaThemeProvider, Toaster } from '@spatika/react';

/** Renders ui inside the providers every screen expects (router + toasts). */
export function renderApp(ui, { route = '/', ...options } = {}) {
  return render(
    <SpatikaThemeProvider defaultTheme="mukta" storageKey="reelstash-test-theme" syncDocument={false}>
      <Toaster position="top-right">
        <MemoryRouter initialEntries={[route]} future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>{ui}</MemoryRouter>
      </Toaster>
    </SpatikaThemeProvider>,
    options,
  );
}

/** A fetch Response stand-in. */
export function jsonResponse(body, { status = 200 } = {}) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  };
}
