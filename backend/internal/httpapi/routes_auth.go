package httpapi

import "github.com/go-chi/chi/v5"

// mountAuth registers sign-in, device pairing, and setup routes.
func (a *API) mountAuth(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Get("/config", a.handleAuthConfig)
		r.Get("/login", a.handleDefaultLogin)
		r.Post("/local-login", a.handleLocalLogin)
		r.Post("/native-login", a.handleNativeLogin)
		r.Get("/server-info", a.handleServerInfo)
		r.Post("/token", a.handleTokenLogin)
		r.Post("/device/start", a.handlePairingStart)
		r.Post("/device/poll", a.handlePairingPoll)
		r.Post("/setup", a.handleSetup)
		r.Route("/plugin/{id}", func(r chi.Router) {
			r.Get("/login", a.handlePluginLogin)
			r.Get("/callback", a.handlePluginCallback)
			r.Post("/login", a.handlePluginPassword)
		})
		r.Post("/logout", a.handleLogout)
		// A plugin may keep a callback address registered with its provider
		// from before it was a plugin, such as /auth/callback.
		r.Get("/*", a.handlePluginCallbackAt)
	})
}
