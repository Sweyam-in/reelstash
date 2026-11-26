// Command server runs the whole ReelStash API in one process: the media
// library API, the admin API and every background job. Local packages,
// SQLite installs and development use it. REELSTASH_ROLE=media or admin
// starts just one half (see cmd/media-server and cmd/admin-server).
package main

import (
	"os"
	_ "time/tzdata" // lets TZ=Area/City work on the slim runtime image, for cron schedules

	"reelstash/backend/internal/httpapi"
	"reelstash/backend/internal/server"
)

func main() {
	if server.QbitInit(os.Args[1:]) {
		return
	}
	server.Run(httpapi.ParseRole(os.Getenv("REELSTASH_ROLE")))
}
