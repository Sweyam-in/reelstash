package app.reelstash.shared.apple

import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.Performer
import app.reelstash.shared.model.Person
import app.reelstash.shared.model.SavedServer
import app.reelstash.shared.model.Series
import app.reelstash.shared.model.ServerInfo
import kotlin.math.roundToInt

/** Why a probed address cannot be used, or null when the app can sign in. */
fun rejectServer(info: ServerInfo): String? = when {
    info.product != "reelstash" -> "That doesn't look like a ReelStash server."
    info.apiVersion < 1 -> "This server is too old for the app. Update ReelStash on the server."
    else -> null
}

fun MediaItem.appleOverview(): String = description?.trim().orEmpty()

fun MediaItem.appleMeta(): String = listOfNotNull(episodeLabel, year?.toString()).joinToString(" · ")

/** Negative when there is nothing to resume. */
fun MediaItem.appleProgress(): Float = progress ?: -1f

fun MediaItem.applePosterId(): Long = posterImageId ?: -1L

fun MediaItem.appleBackdropId(): Long = backdropImageId ?: -1L

fun MediaItem.appleDuration(): Double = durationSeconds ?: files.firstOrNull()?.durationSeconds ?: 0.0

fun MediaItem.applePosition(): Double = positionSeconds ?: 0.0

fun MediaItem.appleStudioId(): Long = studioId ?: -1L

fun MediaItem.appleSeriesId(): Long = seriesId ?: -1L

fun MediaItem.appleMinutes(): Int {
    val seconds = appleDuration()
    if (seconds <= 0) return 0
    return (seconds / 60.0).roundToInt()
}

fun Series.applePosterId(): Long = posterImageId ?: -1L

fun Series.appleBackdropId(): Long = backdropImageId ?: -1L

fun Performer.appleImageId(): Long = profileImageId ?: -1L

fun Person.appleImageId(): Long = profileImageId ?: -1L

fun Person.appleCharacter(): String = character?.trim().orEmpty()

fun SavedServer.appleLibraryId(): Long = libraryId ?: -1L
