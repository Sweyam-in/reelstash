// Command admin-server runs the ReelStash admin API for the admin app, and
// every background job: scans, metadata matching, thumbnails, schedules,
// file watching, downloads and plugin bundles.
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
	server.Run(httpapi.RoleAdmin)
}
