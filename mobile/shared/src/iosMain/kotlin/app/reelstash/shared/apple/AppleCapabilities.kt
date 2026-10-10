package app.reelstash.shared.apple

import app.reelstash.shared.playback.DeviceCapabilities

/**
 * What AVPlayer decodes without the server transcoding. Matroska, AVI, and
 * Opus are left to HLS: AVFoundation does not play them.
 */
object AppleCapabilities {
    fun current() = DeviceCapabilities(
        containers = setOf("mp4", "m4v", "mov"),
        videoCodecs = setOf("h264", "hevc"),
        audioCodecs = setOf("aac", "mp3", "ac3", "eac3"),
    )
}
