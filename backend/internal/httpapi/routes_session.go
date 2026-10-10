package httpapi

import "github.com/go-chi/chi/v5"

// mountSession registers the authenticated account routes and the request middleware they share.
func (a *API) mountSession(r chi.Router) {
	r.Use(a.requireAuth)
	// Enforces the optional PIN lock; /api/me and /api/applock/* are exempt.
	r.Use(a.requireUnlocked)
	r.Use(a.withLibraryScope)

	r.Get("/me", a.handleMe)
	r.Get("/me/library-export", a.exportLibrary)
	r.Post("/me/library-import", a.importLibrary)
	r.Put("/me/password", a.changeMyPassword)
	r.Route("/devices", func(r chi.Router) {
		r.Get("/", a.listDevices)
		r.Delete("/{id}", a.revokeDevice)
		r.Get("/pair", a.lookupPairing)
		r.Post("/pair", a.approvePairing)
	})
	r.Route("/applock", func(r chi.Router) {
		r.Get("/", a.handleAppLockStatus)
		r.Post("/verify", a.handleAppLockVerify)
		r.Post("/lock", a.handleAppLockLock)
		r.Put("/pin", a.handleAppLockSetPin)
		r.Delete("/pin", a.handleAppLockRemovePin)
		r.Put("/settings", a.handleAppLockSettings)
	})

	r.Get("/preferences", a.getPreferences)
	r.Put("/preferences", a.updatePreferences)
	r.Get("/providers", a.listProviders)
	r.Get("/libraries", a.listMyLibraries)
	r.Put("/libraries/default", a.setDefaultLibrary)
}
