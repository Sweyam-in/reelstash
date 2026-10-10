package app.reelstash.shared.apple

import app.reelstash.shared.api.DeviceDescription
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.api.ReelStashClient
import app.reelstash.shared.api.ServerCredentials
import app.reelstash.shared.model.AppLockStatus
import app.reelstash.shared.model.Collection
import app.reelstash.shared.model.CollectionDetail
import app.reelstash.shared.model.DeviceSession
import app.reelstash.shared.model.HomeSection
import app.reelstash.shared.model.Libraries
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.PairingStart
import app.reelstash.shared.model.Performer
import app.reelstash.shared.model.PlaybackUpdate
import app.reelstash.shared.model.SearchHit
import app.reelstash.shared.model.Series
import app.reelstash.shared.model.ServerInfo
import app.reelstash.shared.model.User
import app.reelstash.shared.model.WatchSessionUpdate
import app.reelstash.shared.playback.PlaybackPlan
import app.reelstash.shared.playback.PlaybackPlanner
import io.ktor.client.engine.darwin.Darwin
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/** One page of titles. A concrete type so it survives the Swift bridge (generic pages do not). */
data class MediaPage(val items: List<MediaItem>, val total: Int, val offset: Int)

data class SeriesPage(val items: List<Series>, val total: Int, val offset: Int)

data class PerformerPage(val items: List<Performer>, val total: Int, val offset: Int)

data class HomeBundle(
    val sections: List<HomeSection>,
    val resume: List<MediaItem>,
    val slides: List<MediaItem> = emptyList(),
    val fresh: List<MediaItem> = emptyList(),
    val favorites: List<MediaItem> = emptyList(),
    val people: List<Performer> = emptyList(),
    val nextUp: List<MediaItem>,
)

data class SearchResults(val items: List<SearchHit>)

data class PlaylistList(val items: List<Collection>)

/** Null [session] while pairing is still waiting. */
data class PairPoll(val session: DeviceSession?)

class AppleCancel(private val job: Job) {
    fun cancel() {
        job.cancel()
    }
}

/**
 * The shared client, with callback methods Swift can call. Suspend functions
 * are not exported to Swift, so every request is launched here.
 */
class AppleSession(
    baseUrl: String,
    token: String?,
    libraryId: Long,
    deviceName: String,
    platform: String,
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    private val device = DeviceDescription(deviceName, platform)
    private val client = ReelStashClient(
        baseUrl,
        ServerCredentials(token = token, libraryId = libraryId.takeIf { it >= 0 }),
        Darwin.create(),
    )

    val serverUrl: String get() = client.baseUrl

    private var watchMediaId: Long = -1
    private var watchSessionId: Long = -1

    fun authorization(): String? = client.credentials.token?.let { "Bearer $it" }

    fun unlockHeader(): String? = client.credentials.unlockToken

    fun libraryHeader(): String? = client.credentials.libraryId?.toString()

    fun setLibrary(libraryId: Long) {
        client.credentials.libraryId = libraryId.takeIf { it >= 0 }
    }

    fun posterUrl(mediaId: Long): String = client.urls.artwork(mediaId, app.reelstash.shared.api.MediaUrls.Artwork.Poster)

    fun thumbnailUrl(mediaId: Long): String = client.urls.artwork(mediaId, app.reelstash.shared.api.MediaUrls.Artwork.Thumbnail)

    fun backdropUrl(mediaId: Long): String = client.urls.artwork(mediaId, app.reelstash.shared.api.MediaUrls.Artwork.Backdrop)

    fun performerUrl(id: Long): String = client.urls.performerImage(id)

    fun seriesUrl(id: Long): String = client.urls.seriesImage(id)

    fun seriesBackdropUrl(id: Long): String = client.urls.seriesBackdrop(id)

    fun subtitleUrl(mediaId: Long, trackId: String, fileId: Long): String = client.urls.subtitle(mediaId, trackId, fileId)

    fun serverInfo(onOk: (ServerInfo) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.serverInfo()) }

    fun colorTheme(onOk: (String) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.authConfig().colorTheme) }

    fun passwordSignIn(username: String, password: String, onOk: (DeviceSession) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) { onOk(client.passwordSignIn(username.trim(), password, device)) }

    fun startPairing(onOk: (PairingStart) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.startPairing(device)) }

    fun pollPairing(deviceCode: String, onOk: (PairPoll) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) { onOk(PairPoll(client.pollPairing(deviceCode))) }

    fun signOut(onDone: () -> Unit) {
        scope.launch {
            runCatching { client.signOut() }
            onDone()
        }
    }

    fun me(onOk: (User) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.me()) }

    fun libraries(onOk: (Libraries) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.libraries()) }

    fun appLock(onOk: (AppLockStatus) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.appLockStatus()) }

    fun unlock(pin: String, onOk: (Boolean) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.unlock(pin)) }

    fun loadHome(onOk: (HomeBundle) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val sections = runCatching { client.home().sections }.getOrDefault(emptyList())
        val resume = runCatching { client.continueWatching().items }.getOrDefault(emptyList())
        val next = runCatching { client.nextUp().items }.getOrDefault(emptyList())
        val slides = runCatching { client.media(MediaQuery(hasArtwork = true), 40, 0).items }.getOrDefault(emptyList())
        val fresh = runCatching { client.media(MediaQuery(), 24, 0).items }.getOrDefault(emptyList())
        val favorites = runCatching { client.media(MediaQuery(favorite = true), 16, 0).items }.getOrDefault(emptyList())
        val people = runCatching { client.performers(limit = 24).items }.getOrDefault(emptyList())
        if (sections.isEmpty() && resume.isEmpty() && next.isEmpty() && slides.isEmpty() && fresh.isEmpty()) {
            client.media(MediaQuery(), 1, 0)
        }
        onOk(HomeBundle(sections, resume, slides, fresh, favorites, people, next))
    }

    fun media(
        kind: String,
        sort: String,
        favorite: Boolean,
        watchLater: Boolean,
        reels: Boolean,
        text: String,
        seriesId: Long,
        performerId: Long,
        limit: Int,
        offset: Int,
        onOk: (MediaPage) -> Unit,
        onErr: (String) -> Unit,
    ) = launch(onErr) {
        val page = client.media(
            MediaQuery(
                text = text.takeIf { it.isNotBlank() },
                kind = when (kind) {
                    "movie" -> MediaQuery.Kind.Movie
                    "episode" -> MediaQuery.Kind.Episode
                    else -> null
                },
                sort = when (sort) {
                    "oldest" -> MediaQuery.Sort.Oldest
                    "title" -> MediaQuery.Sort.Title
                    "series" -> MediaQuery.Sort.Series
                    "shuffle" -> MediaQuery.Sort.Shuffle
                    else -> MediaQuery.Sort.Newest
                },
                favorite = favorite,
                watchLater = watchLater,
                reels = reels,
                seriesId = seriesId.takeIf { it >= 0 },
                performerId = performerId.takeIf { it >= 0 },
            ),
            limit,
            offset,
        )
        onOk(MediaPage(page.items, page.total, page.offset))
    }

    /** Portrait titles, shuffled with a stable seed so the next page continues the same feed. */
    fun reels(limit: Int, offset: Int, seed: String, onOk: (MediaPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val page = client.media(
            MediaQuery(reels = true, sort = MediaQuery.Sort.Shuffle, seed = seed.ifBlank { null }),
            limit,
            offset,
        )
        onOk(MediaPage(page.items, page.total, page.offset))
    }

    fun mediaItem(id: Long, onOk: (MediaItem) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.mediaItem(id)) }

    fun studioTitles(studioId: Long, onOk: (MediaPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val page = client.media(MediaQuery(studioId = studioId), 16, 0)
        onOk(MediaPage(page.items, page.total, page.offset))
    }

    fun nextTitle(id: Long, onOk: (MediaPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val item = client.next(id)
        onOk(if (item == null) MediaPage(emptyList(), 0, 0) else MediaPage(listOf(item), 1, 0))
    }

    fun related(id: Long, onOk: (MediaPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val items = client.related(id).items
        onOk(MediaPage(items, items.size, 0))
    }

    fun search(text: String, onOk: (SearchResults) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) { onOk(SearchResults(client.search(text).items)) }

    fun shows(limit: Int, offset: Int, onOk: (SeriesPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val page = client.shows(limit, offset)
        onOk(SeriesPage(page.items, page.total, page.offset))
    }

    fun seriesList(limit: Int, offset: Int, onOk: (SeriesPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val page = client.series(limit, offset)
        onOk(SeriesPage(page.items, page.total, page.offset))
    }

    fun seriesDetail(id: Long, onOk: (Series) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.seriesDetail(id)) }

    fun performers(limit: Int, offset: Int, text: String, onOk: (PerformerPage) -> Unit, onErr: (String) -> Unit) = launch(onErr) {
        val page = client.performers(limit, offset, text.takeIf { it.isNotBlank() })
        onOk(PerformerPage(page.items, page.total, page.offset))
    }

    fun performer(id: Long, onOk: (Performer) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.performer(id)) }

    fun playlists(onOk: (PlaylistList) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(PlaylistList(client.playlists().items)) }

    fun playlist(id: Long, onOk: (CollectionDetail) -> Unit, onErr: (String) -> Unit) = launch(onErr) { onOk(client.playlist(id)) }

    fun setFavorite(id: Long, favorite: Boolean, onOk: (MediaItem) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) { onOk(client.setFavorite(id, favorite)) }

    fun setWatched(id: Long, watched: Boolean, onOk: (MediaItem) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) { onOk(client.setWatched(id, watched)) }

    fun setWatchLater(id: Long, on: Boolean, onOk: () -> Unit, onErr: (String) -> Unit) =
        launch(onErr) {
            client.setWatchLater(id, on)
            onOk()
        }

    fun preparePlayback(mediaId: Long, forceTranscode: Boolean, onOk: (PlaybackPlan) -> Unit, onErr: (String) -> Unit) =
        launch(onErr) {
            val info = client.playbackInfo(mediaId)
            val tracks = runCatching { client.tracks(mediaId, info.fileId) }.getOrNull()
            val prefs = runCatching { client.preferences() }.getOrNull()
            watchMediaId = mediaId
            watchSessionId = -1
            onOk(
                PlaybackPlanner.plan(
                    mediaId = mediaId,
                    info = info,
                    tracks = tracks,
                    prefs = prefs,
                    caps = AppleCapabilities.current(),
                    urls = client.urls,
                    forceTranscode = forceTranscode,
                ),
            )
        }

    fun reportProgress(
        mediaId: Long,
        position: Double,
        watched: Double,
        duration: Double,
        completed: Boolean,
        onErr: (String) -> Unit,
    ) = launch(onErr) {
        client.savePosition(mediaId, PlaybackUpdate(position, completed))
        if (watchMediaId != mediaId) {
            watchMediaId = mediaId
            watchSessionId = -1
        }
        val sessionId = if (watchSessionId >= 0) watchSessionId else client.startWatchSession(mediaId).also { watchSessionId = it }
        client.updateWatchSession(
            sessionId,
            WatchSessionUpdate(
                positionSeconds = position,
                watchedSeconds = watched,
                durationSeconds = duration,
                completed = completed,
            ),
        )
    }

    /** "SignedOut" or "Locked". */
    fun observe(onEvent: (String) -> Unit): AppleCancel {
        val job = scope.launch {
            client.events.collect { onEvent(it.name) }
        }
        return AppleCancel(job)
    }

    fun close() {
        scope.launch {
            client.close()
        }
    }

    private fun launch(onErr: (String) -> Unit, block: suspend () -> Unit) {
        scope.launch {
            try {
                block()
            } catch (error: Throwable) {
                onErr(error.message?.takeIf { it.isNotBlank() } ?: "Something went wrong")
            }
        }
    }
}
