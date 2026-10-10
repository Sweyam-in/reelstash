package app.reelstash.android.player

import android.app.Application
import androidx.annotation.OptIn
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.C
import androidx.media3.common.MediaItem as ExoItem
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import app.reelstash.android.data.ActiveServer
import app.reelstash.android.data.AppContainer
import app.reelstash.android.data.AppState
import app.reelstash.android.ui.common.friendlyMessage
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.PlaybackUpdate
import app.reelstash.shared.model.Preferences
import app.reelstash.shared.model.WatchSessionUpdate
import app.reelstash.shared.playback.PlaybackPlan
import app.reelstash.shared.playback.PlaybackPlanner
import app.reelstash.shared.playback.StreamKind
import app.reelstash.shared.playback.WatchProgress
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class PlayerUiState(
    val item: MediaItem? = null,
    val plan: PlaybackPlan? = null,
    val error: String? = null,
    val upNext: MediaItem? = null,
    /** Seconds until the next title starts; null when there is no countdown. */
    val upNextIn: Int? = null,
    val portrait: Boolean? = null,
)

/**
 * Plays one title at a time and reports progress the way the web player does:
 * the resume point every 15s of movement, plus a watch session counting time
 * actually watched. Ends with an "Up next" countdown when there is a next episode.
 */
@OptIn(UnstableApi::class)
class PlayerViewModel(app: Application, private val container: AppContainer, mediaId: Long) : AndroidViewModel(app) {
    private val server: ActiveServer = (container.state.value as AppState.Ready).server
    private val client = server.client

    val player: ExoPlayer = ExoPlayer.Builder(app)
        .setMediaSourceFactory(DefaultMediaSourceFactory(OkHttpDataSource.Factory(container.mediaHttp)))
        .setSeekBackIncrementMs(10_000)
        .setSeekForwardIncrementMs(30_000)
        .build()

    private val _state = MutableStateFlow(PlayerUiState())
    val state: StateFlow<PlayerUiState> = _state.asStateFlow()

    private var prefs: Preferences? = null
    private var progress = WatchProgress()
    private var sessionId: Long? = null
    private var tickJob: Job? = null
    private var countdownJob: Job? = null
    private var triedTranscode = false

    init {
        player.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) finished()
            }

            override fun onIsPlayingChanged(isPlaying: Boolean) {
                if (isPlaying) ensureSession() else save()
            }

            override fun onPositionDiscontinuity(old: Player.PositionInfo, new: Player.PositionInfo, reason: Int) {
                if (reason == Player.DISCONTINUITY_REASON_SEEK) progress.seeked(new.positionMs / 1000.0)
            }

            override fun onVideoSizeChanged(size: androidx.media3.common.VideoSize) {
                if (size.width > 0 && size.height > 0) _state.update { it.copy(portrait = size.height > size.width) }
            }

            override fun onPlayerError(error: PlaybackException) = playbackFailed(error)
        })
        open(mediaId)
    }

    fun open(mediaId: Long) {
        countdownJob?.cancel()
        tickJob?.cancel()
        sessionId = null
        triedTranscode = false
        _state.value = PlayerUiState()
        viewModelScope.launch {
            try {
                val item = client.mediaItem(mediaId)
                if (prefs == null) prefs = runCatching { client.preferences() }.getOrNull()
                val info = client.playbackInfo(mediaId)
                val tracks = runCatching { client.tracks(mediaId, info.fileId) }.getOrNull()
                val plan = PlaybackPlanner.plan(mediaId, info, tracks, prefs, container.capabilities, client.urls)
                val start = WatchProgress.resumeFrom(item.positionSeconds, item.durationSeconds ?: info.duration)
                _state.update { it.copy(item = item, plan = plan) }
                play(plan, start)
            } catch (e: Exception) {
                _state.update { it.copy(error = friendlyMessage(e)) }
            }
        }
    }

    private fun play(plan: PlaybackPlan, startSeconds: Double) {
        progress = WatchProgress(startSeconds)
        val mediaId = _state.value.item!!.id
        val subtitles = plan.subtitles.map { sub ->
            ExoItem.SubtitleConfiguration.Builder(android.net.Uri.parse(client.urls.subtitle(mediaId, sub.id, plan.fileId)))
                .setMimeType(MimeTypes.TEXT_VTT)
                .setId(sub.id)
                .setLanguage(sub.language)
                .setLabel(sub.label.ifBlank { sub.languageLabel ?: sub.id })
                .setSelectionFlags(
                    (if (sub == plan.initialSubtitle) C.SELECTION_FLAG_DEFAULT else 0) or (if (sub.forced) C.SELECTION_FLAG_FORCED else 0),
                )
                .build()
        }
        val media = ExoItem.Builder()
            .setUri(plan.url)
            .setMediaId(mediaId.toString())
            .setMimeType(if (plan.kind == StreamKind.Hls) MimeTypes.APPLICATION_M3U8 else null)
            .setSubtitleConfigurations(subtitles)
            .build()
        player.trackSelectionParameters = player.trackSelectionParameters.buildUpon()
            .setPreferredAudioLanguage(prefs?.audioLanguage)
            .setPreferredTextLanguage(plan.initialSubtitle?.language)
            .build()
        player.setMediaItem(media, (startSeconds * 1000).toLong())
        player.prepare()
        player.playWhenReady = true
        startTicking()
    }

    private fun startTicking() {
        tickJob?.cancel()
        tickJob = viewModelScope.launch {
            while (isActive) {
                delay(1000)
                if (player.isPlaying && progress.tick(player.currentPosition / 1000.0)) save()
            }
        }
    }

    private fun ensureSession() {
        val id = _state.value.item?.id ?: return
        if (sessionId != null) return
        viewModelScope.launch { sessionId = runCatching { client.startWatchSession(id) }.getOrNull() }
    }

    /** Saves the resume point and the watch session. */
    fun save(completed: Boolean = false) {
        val id = _state.value.item?.id ?: return
        if (player.playbackState == Player.STATE_IDLE && !completed) return
        val position = player.currentPosition / 1000.0
        val duration = player.duration.takeIf { it > 0 }?.div(1000.0) ?: 0.0
        progress.markSaved()
        val session = sessionId
        val watched = progress.watchedSeconds
        viewModelScope.launch {
            withContext(NonCancellable) {
                runCatching { client.savePosition(id, PlaybackUpdate(if (completed) 0.0 else position, completed)) }
                if (session != null && (watched > 0 || completed)) {
                    runCatching { client.updateWatchSession(session, WatchSessionUpdate(position, watched, duration, completed)) }
                }
            }
        }
        if (completed) sessionId = null
    }

    private fun finished() {
        save(completed = true)
        val id = _state.value.item?.id ?: return
        viewModelScope.launch {
            val next = runCatching { client.next(id) }.getOrNull() ?: return@launch
            _state.update { it.copy(upNext = next, upNextIn = UP_NEXT_SECONDS) }
            countdownJob = launch {
                for (s in UP_NEXT_SECONDS downTo 1) {
                    _state.update { it.copy(upNextIn = s) }
                    delay(1000)
                }
                playNext()
            }
        }
    }

    fun playNext() {
        val next = _state.value.upNext ?: return
        open(next.id)
    }

    fun cancelUpNext() {
        countdownJob?.cancel()
        _state.update { it.copy(upNextIn = null) }
    }

    /** A direct-play file the device can't decode after all: ask the server to transcode, from the same spot. */
    private fun playbackFailed(error: PlaybackException) {
        val s = _state.value
        val item = s.item
        val plan = s.plan
        val decodeProblem = error.errorCode in PlaybackException.ERROR_CODE_DECODER_INIT_FAILED..PlaybackException.ERROR_CODE_DECODING_RESOURCES_RECLAIMED ||
            error.errorCode in PlaybackException.ERROR_CODE_PARSING_CONTAINER_MALFORMED..PlaybackException.ERROR_CODE_PARSING_MANIFEST_UNSUPPORTED ||
            error.errorCode == PlaybackException.ERROR_CODE_AUDIO_TRACK_INIT_FAILED
        if (item != null && plan?.kind == StreamKind.Direct && decodeProblem && !triedTranscode) {
            triedTranscode = true
            val at = player.currentPosition / 1000.0
            viewModelScope.launch {
                try {
                    val info = client.playbackInfo(item.id, plan.fileId)
                    if (!info.transcoding) throw error
                    val tracks = runCatching { client.tracks(item.id, plan.fileId) }.getOrNull()
                    val hls = PlaybackPlanner.plan(item.id, info, tracks, prefs, container.capabilities, client.urls, forceTranscode = true)
                    _state.update { it.copy(plan = hls) }
                    play(hls, at)
                } catch (e: Exception) {
                    _state.update { it.copy(error = friendlyMessage(error)) }
                }
            }
            return
        }
        _state.update { it.copy(error = error.localizedMessage ?: "Playback failed") }
    }

    override fun onCleared() {
        save()
        player.release()
    }

    companion object {
        const val UP_NEXT_SECONDS = 10
    }
}
