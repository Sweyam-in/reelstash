package app.reelstash.shared.api

import io.ktor.http.encodeURLParameter

/** Filters and sort for GET /api/media. Null means "don't filter on it". */
data class MediaQuery(
    val text: String? = null,
    val kind: Kind? = null,
    val sort: Sort = Sort.Newest,
    val favorite: Boolean = false,
    val watchLater: Boolean = false,
    val reels: Boolean = false,
    /** Only titles with a poster or backdrop. */
    val hasArtwork: Boolean = false,
    /** Keeps a shuffle in the same order while paging. */
    val seed: String? = null,
    val seriesId: Long? = null,
    val season: Int? = null,
    val performerId: Long? = null,
    val studioId: Long? = null,
) {
    enum class Kind(val wire: String) { Movie("movie"), Episode("episode") }

    /** Wire values match the server; [Newest] sends nothing (its default). */
    enum class Sort(val wire: String?, val label: String) {
        Newest(null, "Recently added"),
        Oldest("oldest", "Oldest"),
        Title("title", "Title"),
        Series("series", "Series order"),
        Shuffle("shuffle", "Shuffle"),
    }

    fun toQueryString(limit: Int, offset: Int): String {
        val params = buildList {
            add("limit" to limit.toString())
            add("offset" to offset.toString())
            text?.takeIf { it.isNotBlank() }?.let { add("q" to it.trim()) }
            kind?.let { add("kind" to it.wire) }
            sort.wire?.let { add("sort" to it) }
            if (favorite) add("favorite" to "true")
            if (watchLater) add("watchLater" to "true")
            if (reels) add("reels" to "true")
            if (hasArtwork) add("hasArtwork" to "true")
            seed?.let { add("seed" to it) }
            seriesId?.let { add("seriesId" to it.toString()) }
            season?.let { add("season" to it.toString()) }
            performerId?.let { add("performerId" to it.toString()) }
            studioId?.let { add("studioId" to it.toString()) }
        }
        return "?" + params.joinToString("&") { (k, v) -> "$k=${v.encodeURLParameter()}" }
    }
}

/** Absolute URLs for images and streams; requests to them need [ServerCredentials.headers]. */
class MediaUrls(private val baseUrl: String) {
    enum class Artwork(val wire: String) { Poster("poster"), Backdrop("backdrop"), Thumbnail("thumbnail") }

    fun artwork(mediaId: Long, kind: Artwork, version: Long? = null) =
        "$baseUrl/api/media/$mediaId/artwork?kind=${kind.wire}" + (version?.let { "&v=$it" } ?: "")

    fun performerImage(id: Long, version: Long? = null) = "$baseUrl/api/performers/$id/image" + (version?.let { "?v=$it" } ?: "")
    fun seriesImage(id: Long, version: Long? = null) = "$baseUrl/api/series/$id/image" + (version?.let { "?v=$it" } ?: "")
    fun seriesBackdrop(id: Long, version: Long? = null) = "$baseUrl/api/series/$id/backdrop" + (version?.let { "?v=$it" } ?: "")

    /** The original file. raw=1 asks the server not to remux it: the app's player reads .mkv itself. */
    fun directStream(mediaId: Long, fileId: Long, audio: Int = 0) =
        "$baseUrl/api/media/$mediaId/stream?fileId=$fileId&raw=1" + (if (audio > 0) "&audio=$audio" else "")

    fun hls(mediaId: Long, fileId: Long, quality: HlsQuality = HlsQuality.Balanced, audio: Int = 0) =
        "$baseUrl/api/media/$mediaId/hls/index.m3u8?fileId=$fileId" +
            (if (quality != HlsQuality.Balanced) "&quality=${quality.wire}" else "") +
            (if (audio > 0) "&audio=$audio" else "")

    fun subtitle(mediaId: Long, trackId: String, fileId: Long? = null) =
        "$baseUrl/api/media/$mediaId/subtitles/$trackId.vtt" + (fileId?.let { "?fileId=$it" } ?: "")
}

enum class HlsQuality(val wire: String, val label: String) {
    High("high", "High"),
    Balanced("balanced", "Balanced"),
    Low("low", "Data saver"),
}
