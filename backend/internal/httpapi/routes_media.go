package httpapi

import "github.com/go-chi/chi/v5"

// mountMedia registers title browsing and playback routes.
func (a *API) mountMedia(r chi.Router) {
	// Browsing, playback and per-user state (favorites, ratings, markers,
	// watch later) are open to every user within their libraries; editing
	// shared metadata is admin-only (admin := r.With(a.requireAdmin)).
	r.Route("/media", func(r chi.Router) {
		r.Get("/", a.listMediaItems)
		r.With(a.requireAdmin).Post("/", a.createMediaItem)
		r.Route("/{id}", func(r chi.Router) {
			r.Use(a.requireMediaAccess)
			admin := r.With(a.requireAdmin)
			r.Get("/", a.getMediaItem)
			r.Patch("/", a.updateMediaItem)
			r.Get("/artwork", a.serveMediaArtwork)
			r.Get("/frame", a.mediaFrame)
			r.Get("/markers", a.listMarkers)
			r.Post("/markers", a.createMarker)
			r.Patch("/markers/{markerId}", a.updateMarker)
			r.Delete("/markers/{markerId}", a.deleteMarker)
			r.Get("/sprite.json", a.spriteInfo)
			r.Get("/sprite.jpg", a.spriteImage)
			r.Get("/sprite.vtt", a.spriteVTT)
			r.Post("/sprite", a.requestSprite)
			r.Get("/preview", a.mediaPreview)
			r.Post("/preview", a.requestPreview)
			r.Get("/stream", a.streamMediaFile)
			r.Get("/playback-info", a.mediaPlaybackInfo)
			r.Get("/tracks", a.mediaTracks)
			r.Get("/subtitles/{track}.vtt", a.mediaSubtitle)
			r.Get("/next", a.nextInSeries)
			r.Get("/related", a.relatedMedia)
			r.Get("/hls/index.m3u8", a.hlsPlaylist)
			r.Get("/hls/seg/{n}.ts", a.hlsSegment)
			admin.Put("/metadata", a.updateMediaMetadata)
			admin.Post("/poster", a.uploadMediaPoster)
			admin.Post("/poster/frame", a.setPosterFromFrame)
			admin.Delete("/poster", a.deleteMediaPoster)
			admin.Post("/backdrop", a.uploadMediaBackdrop)
			admin.Post("/backdrop/frame", a.setBackdropFromFrame)
			admin.Delete("/backdrop", a.deleteMediaBackdrop)
			admin.Get("/match", a.searchMediaMatches)
			admin.Post("/match", a.applyMediaMatch)
			admin.Get("/match/state", a.mediaMatchState)
			admin.Get("/match/linked", a.mediaLinkedRecords)
			admin.Post("/match/unmatch", a.unmatchMedia)
			admin.Post("/links", a.linkMediaProvider)
			admin.Put("/series", a.setMediaSeries)
			admin.Put("/reel", a.setMediaReel)
			admin.Post("/tracks/refresh", a.refreshMediaTracks)
			admin.Get("/subtitles/search", a.searchMediaSubtitles)
			admin.Post("/subtitles", a.downloadMediaSubtitle)
			admin.Delete("/subtitles/{track}", a.deleteMediaSubtitle)
		})
	})
}
