package app.reelstash.shared.playback

import app.reelstash.shared.api.HlsQuality
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.AudioTrack
import app.reelstash.shared.model.PlaybackInfo
import app.reelstash.shared.model.Preferences
import app.reelstash.shared.model.SubtitleTrack
import app.reelstash.shared.model.Tracks

/**
 * What this device can decode. Containers are matched against what the server
 * reports for the file: its extension (mkv, mp4, avi, ...). The server's own `directPlay` flag is about
 * web browsers; a native player handles more (.mkv, HEVC, AC-3 on many TVs),
 * so the app decides for itself from the file's codecs.
 *
 * Codec names are ffprobe's (h264, hevc, vp9, av1, aac, ac3, eac3, opus, ...).
 */
data class DeviceCapabilities(
    val containers: Set<String>,
    val videoCodecs: Set<String>,
    val audioCodecs: Set<String>,
)

enum class StreamKind { Direct, Hls }

data class PlaybackPlan(
    val kind: StreamKind,
    val url: String,
    val fileId: Long,
    /** Position of the chosen audio track among the file's audio tracks (0 = first). */
    val audioIndex: Int,
    /** Sidecar subtitles the player should load as WebVTT. */
    val subtitles: List<SubtitleTrack>,
    /** The subtitle to show at start, or null for none. */
    val initialSubtitle: SubtitleTrack?,
    val reason: String,
)

object PlaybackPlanner {
    fun canDirectPlay(info: PlaybackInfo, caps: DeviceCapabilities): Boolean {
        val containerOk = info.container.split(',').map { it.trim().lowercase() }.any { it in caps.containers }
        val videoOk = info.videoCodec.isEmpty() || info.videoCodec.lowercase() in caps.videoCodecs
        val audioOk = info.audioCodec.isEmpty() || info.audioCodec.lowercase() in caps.audioCodecs
        return containerOk && videoOk && audioOk
    }

    fun plan(
        mediaId: Long,
        info: PlaybackInfo,
        tracks: Tracks?,
        prefs: Preferences?,
        caps: DeviceCapabilities,
        urls: MediaUrls,
        quality: HlsQuality = HlsQuality.Balanced,
        forceTranscode: Boolean = false,
    ): PlaybackPlan {
        val audio = chooseAudio(tracks?.audio.orEmpty(), prefs)
        val chosenAudio = tracks?.audio?.getOrNull(audio)
        val audioOk = chosenAudio?.codec?.let { it.lowercase() in caps.audioCodecs } ?: true
        val direct = !forceTranscode && canDirectPlay(info, caps) && audioOk
        val kind = if (direct || !info.transcoding) StreamKind.Direct else StreamKind.Hls

        // Direct play keeps embedded subtitles inside the file, where the player
        // finds them; HLS segments carry none, so every track comes as WebVTT.
        val subs = tracks?.subtitles.orEmpty().filter { kind == StreamKind.Hls || it.external }
        val initial = chooseSubtitle(subs, chosenAudio, prefs)
        val url = when (kind) {
            StreamKind.Direct -> urls.directStream(mediaId, info.fileId)
            StreamKind.Hls -> urls.hls(mediaId, info.fileId, quality, audio)
        }
        val reason = when {
            direct -> "Direct play"
            !info.transcoding -> "Direct play (server cannot transcode)"
            forceTranscode -> "Transcoding (${quality.label})"
            else -> "Transcoding: ${listOf(info.container, info.videoCodec, chosenAudio?.codec ?: info.audioCodec).filter { it.isNotEmpty() }.joinToString(" / ")}"
        }
        return PlaybackPlan(kind, url, info.fileId, audio, subs, initial, reason)
    }

    /** The preferred-language track, else the file's default, else the first. */
    fun chooseAudio(audio: List<AudioTrack>, prefs: Preferences?): Int {
        if (audio.isEmpty()) return 0
        prefs?.audioLanguage?.let { lang ->
            audio.indexOfFirst { it.language.equals(lang, ignoreCase = true) }.takeIf { it >= 0 }?.let { return it }
        }
        return audio.indexOfFirst { it.default }.takeIf { it >= 0 } ?: 0
    }

    /**
     * Follows the user's subtitle mode: "always" shows their language, "auto"
     * shows it only when the audio is in another language (forced tracks still
     * show), "off" shows nothing.
     */
    fun chooseSubtitle(subs: List<SubtitleTrack>, audio: AudioTrack?, prefs: Preferences?): SubtitleTrack? {
        if (subs.isEmpty()) return null
        val lang = prefs?.subtitleLanguage ?: prefs?.audioLanguage
        val inLang = subs.filter { lang != null && it.language.equals(lang, ignoreCase = true) }
        return when (prefs?.subtitleMode) {
            "always" -> inLang.firstOrNull { !it.forced } ?: inLang.firstOrNull()
            "auto" -> {
                val foreignAudio = audio?.language != null && lang != null && !audio.language.equals(lang, ignoreCase = true)
                if (foreignAudio) inLang.firstOrNull { !it.forced } ?: inLang.firstOrNull() else inLang.firstOrNull { it.forced }
            }
            else -> null
        }
    }
}
