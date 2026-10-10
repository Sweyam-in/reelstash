package app.reelstash.shared

import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.AudioTrack
import app.reelstash.shared.model.PlaybackInfo
import app.reelstash.shared.model.Preferences
import app.reelstash.shared.model.SubtitleTrack
import app.reelstash.shared.model.Tracks
import app.reelstash.shared.playback.DeviceCapabilities
import app.reelstash.shared.playback.PlaybackPlanner
import app.reelstash.shared.playback.StreamKind
import app.reelstash.shared.playback.WatchProgress
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class PlaybackPlannerTest {
    private val phone = DeviceCapabilities(
        containers = setOf("mp4", "mov", "mkv", "webm"),
        videoCodecs = setOf("h264", "hevc", "vp9"),
        audioCodecs = setOf("aac", "mp3", "opus"),
    )
    private val urls = MediaUrls("http://nas.local:8000")
    private fun info(container: String, video: String, audio: String, transcoding: Boolean = true) =
        PlaybackInfo(fileId = 7, container = container, videoCodec = video, audioCodec = audio, duration = 100.0, transcoding = transcoding)

    @Test
    fun mkvWithSupportedCodecsPlaysDirectWithoutRemux() {
        val plan = PlaybackPlanner.plan(1, info("mkv", "hevc", "aac"), null, null, phone, urls)
        assertEquals(StreamKind.Direct, plan.kind)
        assertEquals("http://nas.local:8000/api/media/1/stream?fileId=7&raw=1", plan.url)
    }

    @Test
    fun unsupportedAudioIsTranscoded() {
        val plan = PlaybackPlanner.plan(1, info("mkv", "h264", "dts"), null, null, phone, urls)
        assertEquals(StreamKind.Hls, plan.kind)
        assertTrue(plan.url.startsWith("http://nas.local:8000/api/media/1/hls/index.m3u8?fileId=7"))
    }

    @Test
    fun withoutFfmpegTheServerCanOnlySendTheFile() {
        val plan = PlaybackPlanner.plan(1, info("avi", "mpeg4", "mp3", transcoding = false), null, null, phone, urls)
        assertEquals(StreamKind.Direct, plan.kind)
    }

    @Test
    fun preferredAudioLanguageIsChosenAndSentToHls() {
        val tracks = Tracks(audio = listOf(AudioTrack(0, language = "en", codec = "aac", default = true), AudioTrack(1, language = "ja", codec = "aac")))
        val plan = PlaybackPlanner.plan(1, info("avi", "mpeg4", "aac"), tracks, Preferences(audioLanguage = "ja"), phone, urls)
        assertEquals(1, plan.audioIndex)
        assertTrue(plan.url.endsWith("&audio=1"), plan.url)
    }

    @Test
    fun directPlayLoadsOnlySidecarSubtitles() {
        val subs = listOf(SubtitleTrack("e0", source = "embedded", language = "en"), SubtitleTrack("x1", source = "external", language = "en"))
        val direct = PlaybackPlanner.plan(1, info("mp4", "h264", "aac"), Tracks(subtitles = subs), null, phone, urls)
        assertEquals(listOf("x1"), direct.subtitles.map { it.id })
        val hls = PlaybackPlanner.plan(1, info("avi", "mpeg4", "aac"), Tracks(subtitles = subs), null, phone, urls)
        assertEquals(listOf("e0", "x1"), hls.subtitles.map { it.id })
    }

    @Test
    fun subtitleModes() {
        val en = SubtitleTrack("a", language = "en")
        val enForced = SubtitleTrack("b", language = "en", forced = true)
        val jaAudio = AudioTrack(0, language = "ja")
        val enAudio = AudioTrack(0, language = "en")
        val subs = listOf(enForced, en)
        assertNull(PlaybackPlanner.chooseSubtitle(subs, jaAudio, Preferences(subtitleLanguage = "en", subtitleMode = "off")))
        assertEquals("a", PlaybackPlanner.chooseSubtitle(subs, enAudio, Preferences(subtitleLanguage = "en", subtitleMode = "always"))?.id)
        assertEquals("a", PlaybackPlanner.chooseSubtitle(subs, jaAudio, Preferences(subtitleLanguage = "en", subtitleMode = "auto"))?.id)
        assertEquals("b", PlaybackPlanner.chooseSubtitle(subs, enAudio, Preferences(subtitleLanguage = "en", subtitleMode = "auto"))?.id)
    }

    @Test
    fun watchProgressCountsOnlyContinuousPlayback() {
        val p = WatchProgress()
        p.tick(0.0); p.tick(1.0); p.tick(2.0)
        p.tick(300.0) // a seek
        p.tick(301.0)
        assertEquals(3.0, p.watchedSeconds)
        assertTrue(p.tick(302.0), "15s past the last save asks for a save")
        p.markSaved()
        assertFalse(p.tick(303.0))
        assertEquals(0.0, WatchProgress.resumeFrom(98.0, 100.0))
        assertEquals(40.0, WatchProgress.resumeFrom(40.0, 100.0))
    }

    @Test
    fun mediaQueryEncodesOnlyWhatIsSet() {
        assertEquals("?limit=60&offset=0", MediaQuery().toQueryString(60, 0))
        assertEquals(
            "?limit=20&offset=40&q=star%20wars&kind=movie&sort=title&favorite=true",
            MediaQuery(text = " star wars ", kind = MediaQuery.Kind.Movie, sort = MediaQuery.Sort.Title, favorite = true).toQueryString(20, 40),
        )
    }
}
