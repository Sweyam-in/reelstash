// Command media-server runs the ReelStash media library API: browsing,
// playback, sign-in and per-user state for the media app and the native
// apps. It runs no background jobs; the admin server does.
package main

import (
	_ "time/tzdata"

	"reelstash/backend/internal/httpapi"
	"reelstash/backend/internal/server"
)

func main() { server.Run(httpapi.RoleMedia) }
