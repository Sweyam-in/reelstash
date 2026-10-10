package app.reelstash.android.device

import android.media.MediaCodecList
import android.media.MediaFormat
import app.reelstash.shared.playback.DeviceCapabilities

/**
 * What this device's ExoPlayer can play without the server transcoding, from
 * the decoders the device actually has. Codec names are ffprobe's, as the server reports them.
 */
object AndroidCapabilities {
    // Containers ExoPlayer extracts itself. The server reports the file's
    // extension; ffprobe's format names are kept too in case that changes. Not AVI.
    private val containers = setOf(
        "mp4", "m4v", "mov", "3gp", "mkv", "webm", "ts", "m2ts", "mts", "flv", "ogv", "ogg",
        "matroska", "mpegts",
    )

    private val videoMimes = mapOf(
        "h264" to MediaFormat.MIMETYPE_VIDEO_AVC,
        "hevc" to MediaFormat.MIMETYPE_VIDEO_HEVC,
        "vp8" to MediaFormat.MIMETYPE_VIDEO_VP8,
        "vp9" to MediaFormat.MIMETYPE_VIDEO_VP9,
        "av1" to MediaFormat.MIMETYPE_VIDEO_AV1,
        "mpeg4" to MediaFormat.MIMETYPE_VIDEO_MPEG4,
        "mpeg2video" to MediaFormat.MIMETYPE_VIDEO_MPEG2,
    )

    private val audioMimes = mapOf(
        "aac" to MediaFormat.MIMETYPE_AUDIO_AAC,
        "mp3" to MediaFormat.MIMETYPE_AUDIO_MPEG,
        "opus" to MediaFormat.MIMETYPE_AUDIO_OPUS,
        "vorbis" to MediaFormat.MIMETYPE_AUDIO_VORBIS,
        "flac" to MediaFormat.MIMETYPE_AUDIO_FLAC,
        "ac3" to MediaFormat.MIMETYPE_AUDIO_AC3,
        "eac3" to MediaFormat.MIMETYPE_AUDIO_EAC3,
        "pcm_s16le" to MediaFormat.MIMETYPE_AUDIO_RAW,
    )

    fun detect(): DeviceCapabilities {
        val decoders = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos
            .filterNot { it.isEncoder }
            .flatMap { it.supportedTypes.asList() }
            .map { it.lowercase() }
            .toSet()
        fun supported(map: Map<String, String>) = map.filterValues { it.lowercase() in decoders }.keys
        return DeviceCapabilities(
            containers = containers,
            // h264 and aac decoders are mandatory on Android; keep them even if the list is odd.
            videoCodecs = supported(videoMimes) + "h264",
            audioCodecs = supported(audioMimes) + setOf("aac", "mp3") + "pcm_s16le",
        )
    }
}
