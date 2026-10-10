package app.reelstash.android.ui.reels

import android.app.Application
import android.content.Context
import androidx.annotation.OptIn
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import androidx.media3.common.MimeTypes
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import app.reelstash.android.data.AppContainer
import app.reelstash.android.data.AppState
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.PlaybackUpdate
import app.reelstash.shared.playback.PlaybackPlanner
import app.reelstash.shared.playback.StreamKind
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import androidx.media3.common.MediaItem as ExoItem

enum class ReelSort(val label: String) { Shuffle("Shuffle"), Newest("Newest") }

data class ReelsState(
    val items: List<MediaItem> = emptyList(),
    val loading: Boolean = true,
    val endReached: Boolean = false,
    val error: String? = null,
    val sort: ReelSort = ReelSort.Shuffle,
    val current: Int = 0,
    val muted: Boolean = false,
    val paused: Boolean = false,
    val progress: Float = 0f,
    val durationMs: Long = 0L,
    val failed: Boolean = false,
)

/**
 * The reels feed, as on the web: portrait titles of the selected library,
 * shuffled with a fresh seed each visit (stable while paging) or newest first.
 * One player follows the reel on screen; reels loop, and watching most of one
 * marks it watched, once per visit.
 */
@OptIn(UnstableApi::class)
class ReelsViewModel(app: Application, private val container: AppContainer) : AndroidViewModel(app) {
    private val client = (container.state.value as AppState.Ready).server.client
    private val prefs = app.getSharedPreferences("reels", Context.MODE_PRIVATE)

    val player: ExoPlayer = ExoPlayer.Builder(app)
        .setMediaSourceFactory(DefaultMediaSourceFactory(OkHttpDataSource.Factory(container.mediaHttp)))
        .build().apply { repeatMode = Player.REPEAT_MODE_ONE }

    private val _state = MutableStateFlow(
        ReelsState(
            sort = runCatching { ReelSort.valueOf(prefs.getString("sort", null) ?: "") }.getOrDefault(ReelSort.Shuffle),
            muted = prefs.getBoolean("muted", false),
        ),
    )
    val state: StateFlow<ReelsState> = _state.asStateFlow()

    private var seed = newSeed()
    private var loadJob: Job? = null
    private var playJob: Job? = null
    private val counted = mutableSetOf<Long>()

    init {
        player.volume = if (_state.value.muted) 0f else 1f
        player.addListener(object : Player.Listener {
            override fun onPlayerError(error: androidx.media3.common.PlaybackException) {
                _state.update { it.copy(failed = true) }
            }
        })
        loadMore()
        viewModelScope.launch {
            while (isActive) {
                delay(200)
                val d = player.duration
                if (d > 0) {
                    val fraction = (player.currentPosition.toFloat() / d).coerceIn(0f, 1f)
                    _state.update { it.copy(progress = fraction, durationMs = d) }
                    markWatchedPast(fraction)
                }
            }
        }
    }

    private fun newSeed() = java.util.UUID.randomUUID().toString().take(8)

    fun loadMore() {
        val s = _state.value
        if (loadJob?.isActive == true || s.endReached) return
        _state.update { it.copy(loading = true, error = null) }
        loadJob = viewModelScope.launch {
            runCatching {
                val q = MediaQuery(reels = true, sort = if (s.sort == ReelSort.Shuffle) MediaQuery.Sort.Shuffle else MediaQuery.Sort.Newest, seed = seed)
                client.media(q, limit = PAGE, offset = s.items.size)
            }.onSuccess { page ->
                val wasEmpty = _state.value.items.isEmpty()
                _state.update { it.copy(items = it.items + page.items, loading = false, endReached = it.items.size + page.items.size >= page.total) }
                if (wasEmpty && page.items.isNotEmpty()) show(0)
            }.onFailure { e ->
                _state.update { it.copy(loading = false, error = e.message ?: "Couldn't load reels") }
            }
        }
    }

    fun setSort(sort: ReelSort) {
        if (sort == _state.value.sort) return
        prefs.edit().putString("sort", sort.name).apply()
        seed = newSeed()
        loadJob?.cancel()
        player.stop()
        _state.update { ReelsState(sort = sort, muted = it.muted) }
        loadMore()
    }

    /** Plays the reel now on screen. */
    fun show(index: Int) {
        val item = _state.value.items.getOrNull(index) ?: return
        _state.update { it.copy(current = index, paused = false, progress = 0f, durationMs = 0L, failed = false) }
        if (index >= _state.value.items.size - 4) loadMore()
        playJob?.cancel()
        playJob = viewModelScope.launch {
            runCatching {
                val info = client.playbackInfo(item.id)
                val plan = PlaybackPlanner.plan(item.id, info, null, null, container.capabilities, client.urls)
                val media = ExoItem.Builder().setUri(plan.url).setMediaId(item.id.toString())
                    .setMimeType(if (plan.kind == StreamKind.Hls) MimeTypes.APPLICATION_M3U8 else null).build()
                player.setMediaItem(media)
                player.prepare()
                player.playWhenReady = !_state.value.paused
            }.onFailure { _state.update { it.copy(failed = true) } }
        }
    }

    fun togglePause() {
        val paused = !_state.value.paused
        _state.update { it.copy(paused = paused) }
        player.playWhenReady = !paused
    }

    fun toggleMute() {
        val muted = !_state.value.muted
        prefs.edit().putBoolean("muted", muted).apply()
        player.volume = if (muted) 0f else 1f
        _state.update { it.copy(muted = muted) }
    }

    fun seekTo(fraction: Float) {
        val d = player.duration
        if (d > 0) player.seekTo((d * fraction.coerceIn(0f, 1f)).toLong())
    }

    /** Leaving the tab or the app: stop, and resume where we were on return. */
    fun pauseForBackground() {
        player.playWhenReady = false
    }

    fun resumeFromBackground() {
        if (!_state.value.paused) player.playWhenReady = true
    }

    fun setFavorite(item: MediaItem, on: Boolean) {
        _state.update { s -> s.copy(items = s.items.map { if (it.id == item.id) it.copy(favorite = on) else it }) }
        viewModelScope.launch {
            runCatching { client.setFavorite(item.id, on) }.onFailure {
                _state.update { s -> s.copy(items = s.items.map { if (it.id == item.id) it.copy(favorite = !on) else it }) }
            }
        }
    }

    private fun markWatchedPast(fraction: Float) {
        val item = _state.value.items.getOrNull(_state.value.current) ?: return
        if (fraction > .9f && counted.add(item.id)) {
            viewModelScope.launch { runCatching { client.savePosition(item.id, PlaybackUpdate(0.0, completed = true)) } }
        }
    }

    override fun onCleared() {
        player.release()
    }

    companion object {
        const val PAGE = 20
    }
}
