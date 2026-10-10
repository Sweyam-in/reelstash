package app.reelstash.shared.api

import app.reelstash.shared.model.ApiError
import app.reelstash.shared.model.AuthConfig
import app.reelstash.shared.model.AppLockStatus
import app.reelstash.shared.model.Collection
import app.reelstash.shared.model.CollectionDetail
import app.reelstash.shared.model.DeviceSession
import app.reelstash.shared.model.HomeSections
import app.reelstash.shared.model.Items
import app.reelstash.shared.model.Libraries
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.MediaPatch
import app.reelstash.shared.model.NextItem
import app.reelstash.shared.model.Page
import app.reelstash.shared.model.PairingStart
import app.reelstash.shared.model.PlaybackInfo
import app.reelstash.shared.model.PlaybackUpdate
import app.reelstash.shared.model.Performer
import app.reelstash.shared.model.Preferences
import app.reelstash.shared.model.SearchHit
import app.reelstash.shared.model.Series
import app.reelstash.shared.model.ServerInfo
import app.reelstash.shared.model.Tracks
import app.reelstash.shared.model.UnlockResult
import app.reelstash.shared.model.User
import app.reelstash.shared.model.WatchSessionStart
import app.reelstash.shared.model.WatchSessionUpdate
import io.ktor.client.HttpClient
import io.ktor.client.engine.HttpClientEngine
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.request.HttpRequestBuilder
import io.ktor.client.request.header
import io.ktor.client.request.request
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.client.statement.bodyAsText
import io.ktor.http.ContentType
import io.ktor.http.HttpMethod
import io.ktor.http.contentType
import io.ktor.http.encodeURLParameter
import io.ktor.http.isSuccess
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.serialization.json.Json
import kotlin.concurrent.Volatile

/** Sign-in state for one server, shared by API calls, the player and image loading. */
class ServerCredentials(token: String? = null, unlockToken: String? = null, libraryId: Long? = null) {
    @Volatile var token: String? = token
    @Volatile var unlockToken: String? = unlockToken
    /** The library being browsed; the server narrows listings to it. */
    @Volatile var libraryId: Long? = libraryId

    /** Headers every request to the server needs, for clients outside Ktor (player, images). */
    fun headers(): Map<String, String> = buildMap {
        token?.let { put(HEADER_AUTHORIZATION, "Bearer $it") }
        unlockToken?.let { put(HEADER_UNLOCK, it) }
        libraryId?.let { put(HEADER_LIBRARY, it.toString()) }
    }

    companion object {
        const val HEADER_AUTHORIZATION = "Authorization"
        const val HEADER_UNLOCK = "X-Reelstash-Unlock"
        const val HEADER_LIBRARY = "X-Library"
    }
}

open class ApiException(val status: Int, message: String) : Exception(message)
class UnauthorizedException(message: String) : ApiException(401, message)
class LockedException(message: String) : ApiException(423, message)

/** Something the UI must react to wherever it happens: sign-in ended, or the PIN lock engaged. */
enum class SessionEvent { SignedOut, Locked }

private const val EMPTY = "{}"

val ReelStashJson = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
}

/**
 * The ReelStash HTTP API, limited to what a media app does: browse, play and
 * keep per-user state. Admin endpoints are deliberately absent.
 */
class ReelStashClient(
    baseUrl: String,
    val credentials: ServerCredentials,
    engine: HttpClientEngine,
) {
    val baseUrl: String = baseUrl.trimEnd('/')
    val urls = MediaUrls(this.baseUrl)

    private val _events = MutableSharedFlow<SessionEvent>(extraBufferCapacity = 4)
    val events: SharedFlow<SessionEvent> = _events

    private val http = HttpClient(engine) {
        expectSuccess = false
        install(HttpTimeout) {
            connectTimeoutMillis = 10_000
            requestTimeoutMillis = 30_000
        }
    }

    // --- Server and sign-in (no session needed) ---------------------------

    suspend fun serverInfo(): ServerInfo = call(HttpMethod.Get, "/auth/server-info", authed = false)

    /** The admin-chosen library palette. Public, so the sign-in screen can match. */
    suspend fun authConfig(): AuthConfig = call(HttpMethod.Get, "/auth/config", authed = false)

    suspend fun passwordSignIn(username: String, password: String, device: DeviceDescription): DeviceSession =
        call(HttpMethod.Post, "/auth/token", authed = false, json = enc(TokenRequest(username, password, device.name, device.platform)))

    suspend fun startPairing(device: DeviceDescription): PairingStart =
        call(HttpMethod.Post, "/auth/device/start", authed = false, json = enc(DeviceBody(device.name, device.platform)))

    /** Null while the pairing waits for approval; throws [ApiException] 404 once it expired. */
    suspend fun pollPairing(deviceCode: String): DeviceSession? {
        val response = send(HttpMethod.Post, "/auth/device/poll", authed = false, json = enc(DeviceCodeBody(deviceCode)))
        if (response.status.value == 202) return null
        return decode(response)
    }

    suspend fun signOut() {
        runCatching { send(HttpMethod.Post, "/auth/logout", json = EMPTY) }
    }

    // --- Account -----------------------------------------------------------

    suspend fun me(): User = call(HttpMethod.Get, "/api/me")
    suspend fun libraries(): Libraries = call(HttpMethod.Get, "/api/libraries")
    suspend fun preferences(): Preferences = call(HttpMethod.Get, "/api/preferences")

    suspend fun appLockStatus(): AppLockStatus = call(HttpMethod.Get, "/api/applock")

    /** Checks the PIN; on success the unlock token is kept for later requests. */
    suspend fun unlock(pin: String): Boolean {
        val result: UnlockResult = call(HttpMethod.Post, "/api/applock/verify", json = enc(PinBody(pin)))
        if (result.valid) result.unlockToken?.let { credentials.unlockToken = it }
        return result.valid
    }

    // --- Browsing ----------------------------------------------------------

    suspend fun home(): HomeSections = call(HttpMethod.Get, "/api/recommendations/home")
    suspend fun continueWatching(): Items<MediaItem> = call(HttpMethod.Get, "/api/playback/continue")
    suspend fun nextUp(): Items<MediaItem> = call(HttpMethod.Get, "/api/playback/next-up")

    suspend fun media(query: MediaQuery, limit: Int = 60, offset: Int = 0): Page<MediaItem> =
        call(HttpMethod.Get, "/api/media" + query.toQueryString(limit, offset))

    suspend fun mediaItem(id: Long): MediaItem = call(HttpMethod.Get, "/api/media/$id")
    suspend fun related(id: Long): Items<MediaItem> = call(HttpMethod.Get, "/api/media/$id/related")
    suspend fun next(id: Long): MediaItem? = call<NextItem>(HttpMethod.Get, "/api/media/$id/next").item

    suspend fun search(text: String): Items<SearchHit> = call(HttpMethod.Get, "/api/search?q=${text.encodeURLParameter()}")

    suspend fun shows(limit: Int = 60, offset: Int = 0): Page<Series> =
        call(HttpMethod.Get, "/api/series?variant=shows&limit=$limit&offset=$offset")

    suspend fun series(limit: Int = 60, offset: Int = 0): Page<Series> =
        call(HttpMethod.Get, "/api/series?limit=$limit&offset=$offset")

    suspend fun seriesDetail(id: Long): Series = call(HttpMethod.Get, "/api/series/$id")

    suspend fun performers(limit: Int = 60, offset: Int = 0, text: String? = null): Page<Performer> =
        call(HttpMethod.Get, "/api/performers?limit=$limit&offset=$offset" + (text?.let { "&q=${it.encodeURLParameter()}" } ?: ""))

    suspend fun performer(id: Long): Performer = call(HttpMethod.Get, "/api/performers/$id")

    suspend fun playlists(): Items<Collection> = call(HttpMethod.Get, "/api/collections")
    suspend fun playlist(id: Long, limit: Int = 100, offset: Int = 0): CollectionDetail =
        call(HttpMethod.Get, "/api/collections/$id?limit=$limit&offset=$offset")

    // --- Personal state ----------------------------------------------------

    suspend fun setFavorite(id: Long, favorite: Boolean): MediaItem = call(HttpMethod.Patch, "/api/media/$id", json = enc(MediaPatch(favorite = favorite)))
    /** Your rating on the server's 0–10 scale (the apps show 0–5 stars: stars × 2); 0 clears it. */
    suspend fun setRating(id: Long, rating: Int): MediaItem = call(HttpMethod.Patch, "/api/media/$id", json = enc(MediaPatch(rating = rating)))
    suspend fun setWatched(id: Long, watched: Boolean): MediaItem = call(HttpMethod.Patch, "/api/media/$id", json = enc(MediaPatch(watched = watched)))

    suspend fun setWatchLater(id: Long, on: Boolean) {
        checked(send(if (on) HttpMethod.Put else HttpMethod.Delete, "/api/watch-later/$id", json = if (on) EMPTY else null))
    }

    // --- Playback ------------------------------------------------------------

    suspend fun playbackInfo(id: Long, fileId: Long? = null): PlaybackInfo =
        call(HttpMethod.Get, "/api/media/$id/playback-info" + (fileId?.let { "?fileId=$it" } ?: ""))

    suspend fun tracks(id: Long, fileId: Long? = null): Tracks =
        call(HttpMethod.Get, "/api/media/$id/tracks" + (fileId?.let { "?fileId=$it" } ?: ""))

    suspend fun savePosition(id: Long, update: PlaybackUpdate) {
        checked(send(HttpMethod.Put, "/api/playback/$id", json = enc(update)))
    }

    suspend fun startWatchSession(id: Long): Long = call<WatchSessionStart>(HttpMethod.Post, "/api/playback/$id/session", json = EMPTY).sessionId

    suspend fun updateWatchSession(sessionId: Long, update: WatchSessionUpdate) {
        checked(send(HttpMethod.Put, "/api/playback/session/$sessionId", json = enc(update)))
    }

    // --- Plumbing ------------------------------------------------------------

    private suspend inline fun <reified T> call(method: HttpMethod, path: String, authed: Boolean = true, json: String? = null): T =
        decode(send(method, path, authed, json))

    private inline fun <reified B> enc(body: B): String = ReelStashJson.encodeToString(body)

    private suspend inline fun <reified T> decode(response: HttpResponse): T {
        checked(response)
        return ReelStashJson.decodeFromString(response.bodyAsText())
    }

    private suspend fun send(method: HttpMethod, path: String, authed: Boolean = true, json: String? = null): HttpResponse {
        val response = http.request(baseUrl + path) {
            this.method = method
            if (authed) applyCredentials()
            if (json != null) {
                contentType(ContentType.Application.Json)
                setBody(json)
            }
        }
        response.headers[ServerCredentials.HEADER_UNLOCK]?.let { credentials.unlockToken = it }
        return response
    }

    private fun HttpRequestBuilder.applyCredentials() {
        credentials.headers().forEach { (k, v) -> header(k, v) }
    }

    private suspend fun checked(response: HttpResponse) {
        if (response.status.isSuccess()) return
        val message = runCatching { ReelStashJson.decodeFromString<ApiError>(response.bodyAsText()).message }
            .getOrNull()?.takeIf { it.isNotBlank() } ?: response.status.description
        when (response.status.value) {
            401 -> {
                _events.tryEmit(SessionEvent.SignedOut)
                throw UnauthorizedException(message)
            }
            423 -> {
                credentials.unlockToken = null
                _events.tryEmit(SessionEvent.Locked)
                throw LockedException(message)
            }
            else -> throw ApiException(response.status.value, message)
        }
    }

    fun close() = http.close()
}

/** What the app tells the server about itself, shown in the user's device list. */
data class DeviceDescription(val name: String, val platform: String)

@kotlinx.serialization.Serializable
private data class TokenRequest(val username: String, val password: String, val deviceName: String, val platform: String)

@kotlinx.serialization.Serializable
private data class DeviceBody(val deviceName: String, val platform: String)

@kotlinx.serialization.Serializable
private data class DeviceCodeBody(val deviceCode: String)

@kotlinx.serialization.Serializable
private data class PinBody(val pin: String)
