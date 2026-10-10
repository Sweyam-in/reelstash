package app.reelstash.shared.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// JSON shapes of the ReelStash server's app-facing API. Only the fields the app
// uses are declared; unknown fields are ignored, so newer servers stay compatible.

@Serializable
data class AuthConfig(
    val colorTheme: String = "grove",
)

/** Ids match the web library themes. An unknown id stays on Grove. */
fun normalizeColorTheme(id: String?): String =
    id?.takeIf { it in COLOR_THEME_IDS } ?: "grove"

private val COLOR_THEME_IDS = setOf("grove", "lagoon", "ember", "iris", "aurora")

@Serializable
data class ServerInfo(
    val product: String = "",
    val name: String = "ReelStash",
    val version: String = "",
    val apiVersion: Int = 0,
    val passwordLogin: Boolean = false,
    val pairing: Boolean = false,
    val plugins: List<SignInPlugin> = emptyList(),
)

@Serializable
data class SignInPlugin(val id: String, val name: String = "", val label: String = "", val type: String = "")

@Serializable
data class User(
    val id: Long,
    val displayName: String = "",
    val email: String = "",
    val avatarUrl: String = "",
    val isAdmin: Boolean = false,
)

@Serializable
data class DeviceSession(val token: String, val expiresAt: String = "", val user: User)

@Serializable
data class PairingStart(
    val deviceCode: String,
    val userCode: String,
    val verificationUrl: String,
    val expiresIn: Int,
    val interval: Int = 3,
)

@Serializable
data class Library(val id: Long, val name: String, val mode: String = MODE_ENTERTAINMENT, val locked: Boolean = false) {
    val isEntertainment get() = mode == MODE_ENTERTAINMENT

    companion object {
        const val MODE_ENTERTAINMENT = "entertainment"
        const val MODE_PRIVATE = "private"
    }
}

@Serializable
data class Libraries(val defaultLibraryId: Long? = null, val items: List<Library> = emptyList())

@Serializable
data class Person(
    val id: Long,
    val name: String,
    val profileImageId: Long? = null,
    val character: String? = null,
    val job: String? = null,
)

@Serializable
data class MediaFile(
    val id: Long,
    val fileName: String = "",
    val extension: String = "",
    val fileSize: Long = 0,
    val width: Int? = null,
    val height: Int? = null,
    val videoCodec: String? = null,
    val audioCodec: String? = null,
    val container: String? = null,
    val durationSeconds: Double? = null,
    val available: Boolean = true,
)

@Serializable
data class MediaItem(
    val id: Long,
    val title: String,
    val originalTitle: String? = null,
    val studioId: Long? = null,
    val studioName: String? = null,
    val seriesId: Long? = null,
    val seriesName: String? = null,
    val kind: String? = null,
    val seasonNumber: Int? = null,
    val episodeNumber: Int? = null,
    val description: String? = null,
    val releaseDate: String? = null,
    val productionYear: Int? = null,
    val durationSeconds: Double? = null,
    val favorite: Boolean = false,
    val rating: Int? = null,
    val myRating: Int? = null,
    val watched: Boolean = false,
    val watchLater: Boolean = false,
    val dateAdded: String? = null,
    val posterImageId: Long? = null,
    val backdropImageId: Long? = null,
    val performers: List<String> = emptyList(),
    val cast: List<Person> = emptyList(),
    val crew: List<Person> = emptyList(),
    val categories: List<String> = emptyList(),
    val tags: List<String> = emptyList(),
    val files: List<MediaFile> = emptyList(),
    val positionSeconds: Double? = null,
    val reason: String? = null,
) {
    /** "S1 · E3" for episodes, else null. */
    val episodeLabel: String?
        get() = when {
            seasonNumber != null && episodeNumber != null -> "S$seasonNumber · E$episodeNumber"
            episodeNumber != null -> "E$episodeNumber"
            else -> null
        }

    val year: Int? get() = productionYear ?: releaseDate?.take(4)?.toIntOrNull()

    /** Fraction watched, for progress bars; null when there is nothing to resume. */
    val progress: Float?
        get() {
            val pos = positionSeconds ?: return null
            val dur = durationSeconds ?: files.firstOrNull()?.durationSeconds ?: return null
            if (pos <= 0 || dur <= 0) return null
            return (pos / dur).toFloat().coerceIn(0f, 1f)
        }

    val playable: Boolean get() = files.isEmpty() || files.any { it.available }
}

@Serializable
data class Page<T>(val items: List<T> = emptyList(), val limit: Int = 0, val offset: Int = 0, val total: Int = 0)

@Serializable
data class Items<T>(val items: List<T> = emptyList())

@Serializable
data class HomeSection(val key: String, val title: String, val subtitle: String? = null, val items: List<MediaItem> = emptyList())

@Serializable
data class HomeSections(val sections: List<HomeSection> = emptyList())

@Serializable
data class NextItem(val item: MediaItem? = null)

@Serializable
data class SearchHit(val type: String, val id: Long, val label: String) {
    companion object {
        const val MEDIA = "media_item"
        const val PERFORMER = "performer"
        const val STUDIO = "studio"
        const val SERIES = "series"
    }
}

@Serializable
data class Series(
    val id: Long,
    val name: String,
    val description: String? = null,
    val studioName: String? = null,
    val posterImageId: Long? = null,
    val backdropImageId: Long? = null,
    val mediaCount: Long = 0,
    val seasonCount: Long = 0,
    val kind: String? = null,
)

@Serializable
data class Performer(
    val id: Long,
    val name: String,
    val profileImageId: Long? = null,
    val mediaCount: Long = 0,
    val favorite: Boolean = false,
)

@Serializable
data class Studio(val id: Long, val name: String, val mediaCount: Long = 0)

@Serializable
data class Collection(
    val id: Long,
    val name: String,
    val kind: String = "MANUAL",
    val mediaCount: Int = 0,
    val pinned: Boolean = false,
    val covers: List<CollectionCover> = emptyList(),
) {
    val smart: Boolean get() = kind.equals("SMART", ignoreCase = true)
}

@Serializable
data class CollectionCover(val id: Long)

@Serializable
data class CollectionDetail(
    val collection: Collection,
    val items: List<MediaItem> = emptyList(),
    val limit: Int = 0,
    val offset: Int = 0,
    val total: Int = 0,
)

@Serializable
data class PlaybackInfo(
    val fileId: Long,
    val container: String = "",
    val videoCodec: String = "",
    val audioCodec: String = "",
    val duration: Double = 0.0,
    val directPlay: Boolean = false,
    val transcoding: Boolean = false,
)

@Serializable
data class AudioTrack(
    val index: Int,
    val language: String? = null,
    val languageLabel: String? = null,
    val codec: String? = null,
    val channels: Int = 0,
    val title: String? = null,
    val default: Boolean = false,
    val label: String = "",
)

@Serializable
data class SubtitleTrack(
    val id: String,
    val source: String = "",
    val language: String? = null,
    val languageLabel: String? = null,
    val codec: String? = null,
    val title: String? = null,
    val default: Boolean = false,
    val forced: Boolean = false,
    val label: String = "",
) {
    val external: Boolean get() = source == "external"
}

@Serializable
data class Tracks(
    val fileId: Long = 0,
    val videoCodec: String? = null,
    val audio: List<AudioTrack> = emptyList(),
    val subtitles: List<SubtitleTrack> = emptyList(),
)

@Serializable
data class Preferences(
    val audioLanguage: String? = null,
    val subtitleLanguage: String? = null,
    /** "off", "auto" (foreign audio only) or "always". */
    val subtitleMode: String = "off",
)

@Serializable
data class WatchSessionStart(val sessionId: Long)

@Serializable
data class WatchSessionUpdate(
    val positionSeconds: Double,
    val watchedSeconds: Double,
    val durationSeconds: Double,
    val completed: Boolean,
)

@Serializable
data class PlaybackUpdate(val positionSeconds: Double, val completed: Boolean)

@Serializable
data class AppLockStatus(
    val hasPin: Boolean = false,
    val locked: Boolean = false,
    val librariesLocked: Boolean = false,
    val idleMinutes: Int = 0,
    /** "app" (the whole app) or "libraries" (only libraryIds). */
    val scope: String = "app",
    val libraryIds: List<Long> = emptyList(),
)

@Serializable
data class UnlockResult(val valid: Boolean = false, val unlockToken: String? = null)

@Serializable
data class MediaPatch(
    val favorite: Boolean? = null,
    val rating: Int? = null,
    val watched: Boolean? = null,
)

@Serializable
internal data class ApiError(@SerialName("error") val message: String = "")
