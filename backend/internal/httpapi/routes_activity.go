package httpapi

import "github.com/go-chi/chi/v5"

// mountActivity registers search, duplicates, playback state, notifications, files, and downloads.
func (a *API) mountActivity(r chi.Router) {
	r.Get("/search", a.handleSearch)
	r.Route("/duplicates", func(r chi.Router) {
		r.Use(a.requireAdmin)
		r.Get("/", a.listDuplicateGroups)
		r.Post("/{id}/dismiss", a.dismissDuplicateGroup)
		r.Post("/{id}/keep", a.keepDuplicate)
		r.Get("/trash", a.listTrash)
		r.Post("/trash/{trashId}/restore", a.restoreTrash)
	})
	titleAccess := a.requireMediaParamAccess("mediaItemID")
	r.Route("/playback", func(r chi.Router) {
		r.Get("/continue", a.listContinueWatching)
		r.Get("/next-up", a.listNextUp)
		r.With(titleAccess).Put("/{mediaItemID}", a.updatePlaybackState)
		r.With(titleAccess).Post("/{mediaItemID}/session", a.startWatchSession)
		r.Put("/session/{sessionID}", a.updateWatchSession)
	})
	r.Route("/watch-later", func(r chi.Router) {
		r.With(titleAccess).Put("/{mediaItemID}", a.addWatchLater)
		r.Delete("/{mediaItemID}", a.removeWatchLater)
	})
	r.Route("/notifications", func(r chi.Router) {
		r.Get("/", a.listNotifications)
		r.Post("/read", a.markNotificationsRead)
	})
	r.Get("/events", a.handleEvents)
	r.Get("/recommendations/home", a.homeRecommendations)
	r.Get("/activity", a.watchActivity)
	r.Route("/files", func(r chi.Router) {
		r.Get("/", a.listFiles)
		r.Get("/locations", a.listFileLocations)
		r.Get("/download", a.downloadFileEntry)
		r.Get("/nfo", a.getFileNFO)
	})

	r.Route("/downloads", a.downloadsRoutes)
}
