package app.reelstash.android.ui.reels

import android.app.Application
import androidx.annotation.OptIn
import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.pager.VerticalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.VolumeOff
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.FavoriteBorder
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.Smartphone
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalViewConfiguration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import app.reelstash.android.container
import app.reelstash.android.ui.common.MediaArt
import app.reelstash.android.ui.common.MessageBox
import app.reelstash.android.ui.common.displayTitle
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.BrandOrange
import app.reelstash.android.ui.theme.ReelStashTheme
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.MediaItem
import kotlin.math.abs
import kotlinx.coroutines.launch

/** Vertical, full-screen reels: swipe up for the next, tap to pause, double-tap to love. */
@Composable
fun ReelsScreen(nav: Navigator) {
    val context = LocalContext.current
    val server = LocalServer.current
    val vm: ReelsViewModel = viewModel(key = "reels:${server.id}:${server.saved.libraryId}") {
        ReelsViewModel(context.applicationContext as Application, context.container)
    }
    val state by vm.state.collectAsStateWithLifecycle()

    // Pause when the tab or the app goes away; carry on when it returns.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle) {
        val observer = LifecycleEventObserver { _, e ->
            when (e) {
                Lifecycle.Event.ON_PAUSE -> vm.pauseForBackground()
                Lifecycle.Event.ON_RESUME -> vm.resumeFromBackground()
                else -> {}
            }
        }
        lifecycle.addObserver(observer)
        onDispose {
            lifecycle.removeObserver(observer)
            vm.pauseForBackground()
        }
    }

    ReelStashTheme(dark = true) {
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            when {
                state.items.isEmpty() && state.loading -> CircularProgressIndicator(Modifier.align(Alignment.Center), color = Color.White, strokeWidth = 3.dp)
                state.items.isEmpty() && state.error != null -> MessageBox("Couldn't load reels", state.error, actionLabel = "Try again", onAction = vm::loadMore)
                state.items.isEmpty() -> MessageBox(
                    "No reels yet", "Vertical videos in this library show up here.", icon = Icons.Outlined.Smartphone,
                )
                else -> Feed(vm, state, server.client.urls, nav)
            }
            SortToggle(state.sort, vm::setSort, Modifier.align(Alignment.TopCenter).statusBarsPadding().padding(top = 10.dp))
        }
    }
}

@Composable
private fun Feed(vm: ReelsViewModel, state: ReelsState, urls: MediaUrls, nav: Navigator) {
    val pager = rememberPagerState(initialPage = state.current) { state.items.size }
    LaunchedEffect(pager) {
        snapshotFlow { pager.settledPage }.collect { if (it != vm.state.value.current) vm.show(it) }
    }
    VerticalPager(pager, Modifier.fillMaxSize(), key = { state.items[it].id }, beyondViewportPageCount = 1) { page ->
        Reel(vm, state.items[page], page == state.current && page == pager.settledPage, state, urls, nav)
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun Reel(vm: ReelsViewModel, item: MediaItem, active: Boolean, state: ReelsState, urls: MediaUrls, nav: Navigator) {
    val scope = rememberCoroutineScope()
    val heart = remember { Animatable(0f) }

    Box(
        Modifier.fillMaxSize().pointerInput(item.id) {
            detectTapGestures(
                onTap = { if (active) vm.togglePause() },
                onDoubleTap = {
                    if (!item.favorite) vm.setFavorite(item, true)
                    scope.launch {
                        heart.snapTo(.4f)
                        heart.animateTo(1.15f, spring(Spring.DampingRatioMediumBouncy))
                        heart.animateTo(0f, tween(450, delayMillis = 350))
                    }
                },
            )
        },
    ) {
        // The thumbnail shows until the video has a frame, and for reels off screen.
        MediaArt(item, urls.artwork(item.id, MediaUrls.Artwork.Thumbnail), Modifier.fillMaxSize(), glyphSize = 200.dp)
        if (active) {
            AndroidView(
                factory = { ctx ->
                    PlayerView(ctx).apply {
                        useController = false
                        resizeMode = AspectRatioFrameLayout.RESIZE_MODE_ZOOM
                        setShutterBackgroundColor(android.graphics.Color.TRANSPARENT)
                        player = vm.player
                    }
                },
                onRelease = { it.player = null },
                modifier = Modifier.fillMaxSize(),
            )
        }
        // Shade top and bottom so the overlays read over any video.
        Box(Modifier.fillMaxSize().background(Brush.verticalGradient(0f to Color.Black.copy(alpha = .45f), .18f to Color.Transparent, .62f to Color.Transparent, 1f to Color.Black.copy(alpha = .7f))))

        if (active && state.paused) {
            Icon(Icons.Filled.PlayArrow, "Paused", tint = Color.White.copy(alpha = .85f), modifier = Modifier.align(Alignment.Center).size(72.dp))
        }
        if (active && state.failed) {
            Text(
                "This video can't be played here.", color = Color.White, style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.align(Alignment.Center).clip(CircleShape).background(Color.Black.copy(alpha = .6f)).padding(horizontal = 16.dp, vertical = 10.dp),
            )
        }
        if (heart.value > 0f) {
            Icon(Icons.Filled.Favorite, null, tint = BrandOrange, modifier = Modifier.align(Alignment.Center).size(110.dp).scale(heart.value))
        }

        // Title and people, bottom left (Instagram's caption spot), above the seek bar.
        Column(Modifier.align(Alignment.BottomStart).fillMaxWidth(.78f).padding(start = 16.dp, end = 8.dp, bottom = 72.dp)) {
            Text(displayTitle(item), color = Color.White, style = MaterialTheme.typography.titleLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            val people = item.performers.take(3).joinToString(" · ")
            if (people.isNotEmpty()) {
                Spacer(Modifier.height(4.dp))
                Text(people, color = Color.White.copy(alpha = .78f), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }

        // The action rail, bottom right.
        Column(Modifier.align(Alignment.BottomEnd).padding(end = 10.dp, bottom = 80.dp), verticalArrangement = Arrangement.spacedBy(18.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            RailButton(if (item.favorite) Icons.Filled.Favorite else Icons.Filled.FavoriteBorder, if (item.favorite) "Loved" else "Love", if (item.favorite) BrandOrange else Color.White) {
                vm.setFavorite(item, !item.favorite)
            }
            RailButton(if (state.muted) Icons.AutoMirrored.Filled.VolumeOff else Icons.AutoMirrored.Filled.VolumeUp, if (state.muted) "Muted" else "Sound", Color.White, vm::toggleMute)
            RailButton(Icons.Outlined.Info, "Details", Color.White) { nav.openMedia(item.id) }
        }

        if (active) {
            ReelScrubber(
                progress = state.progress,
                durationMs = state.durationMs,
                onSeek = vm::seekTo,
                modifier = Modifier.align(Alignment.BottomCenter),
            )
        }
    }
}

/** A seek bar with a tall hit area and a thumb, so a finger can grab it without aiming at the line. */
@Composable
private fun ReelScrubber(progress: Float, durationMs: Long, onSeek: (Float) -> Unit, modifier: Modifier = Modifier) {
    var drag by remember { mutableStateOf<Float?>(null) }
    val shown = (drag ?: progress).coerceIn(0f, 1f)
    val dragging = drag != null
    val touchSlop = LocalViewConfiguration.current.touchSlop
    val density = LocalDensity.current
    val inset = 16.dp
    val insetPx = with(density) { inset.toPx() }
    Column(modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
        if (dragging && durationMs > 0L) {
            val at = (durationMs * shown.toDouble()).toLong()
            Text("${clock(at)}  /  ${clock(durationMs)}", color = Color.White, style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(bottom = 6.dp))
        }
        BoxWithConstraints(
            Modifier.fillMaxWidth().height(56.dp).pointerInput(touchSlop, insetPx) {
                awaitEachGesture {
                    val down = awaitFirstDown()
                    down.consume()
                    val usable = (size.width - insetPx * 2).coerceAtLeast(1f)
                    fun fractionAt(x: Float) = ((x - insetPx) / usable).coerceIn(0f, 1f)
                    val origin = down.position.x
                    var fraction = fractionAt(origin)
                    var moved = false
                    drag = fraction
                    while (true) {
                        val event = awaitPointerEvent()
                        val change = event.changes.firstOrNull { it.id == down.id } ?: break
                        if (!change.pressed) {
                            onSeek(fraction)
                            change.consume()
                            break
                        }
                        if (abs(change.position.x - origin) > touchSlop) moved = true
                        fraction = fractionAt(change.position.x)
                        drag = fraction
                        if (moved) onSeek(fraction)
                        change.consume()
                    }
                    drag = null
                }
            },
        ) {
            val thumb = if (dragging) 22.dp else 16.dp
            val track = if (dragging) 6.dp else 4.dp
            Box(Modifier.align(Alignment.CenterStart).padding(horizontal = inset).fillMaxWidth().height(track).clip(CircleShape).background(Color.White.copy(alpha = .35f)))
            Box(Modifier.align(Alignment.CenterStart).padding(start = inset).width((maxWidth - inset * 2) * shown).height(track).clip(CircleShape).background(Color.White))
            Box(
                Modifier.align(Alignment.CenterStart).padding(start = inset).offset(x = (maxWidth - inset * 2) * shown - thumb / 2).size(thumb).shadow(4.dp, CircleShape).clip(CircleShape).background(Color.White),
            )
        }
    }
}

private fun clock(ms: Long): String {
    val total = (ms / 1000).coerceAtLeast(0)
    val hours = total / 3600
    val minutes = (total % 3600) / 60
    val seconds = total % 60
    return if (hours > 0) "%d:%02d:%02d".format(hours, minutes, seconds) else "%d:%02d".format(minutes, seconds)
}

@Composable
private fun RailButton(icon: ImageVector, label: String, tint: Color, onClick: () -> Unit) {
    Column(horizontalAlignment = Alignment.CenterHorizontally, modifier = Modifier.clip(CircleShape).clickable(onClick = onClick).padding(4.dp)) {
        Icon(icon, label, tint = tint, modifier = Modifier.size(30.dp))
        Spacer(Modifier.height(3.dp))
        Text(label, color = Color.White, style = MaterialTheme.typography.labelSmall)
    }
}

/** Shuffle / Newest, as the white pill toggle at the top of the web's reels. */
@Composable
private fun SortToggle(sort: ReelSort, onSort: (ReelSort) -> Unit, modifier: Modifier) {
    Row(modifier.clip(CircleShape).background(Color.Black.copy(alpha = .35f)).padding(3.dp), horizontalArrangement = Arrangement.spacedBy(2.dp)) {
        ReelSort.entries.forEach { s ->
            val on = s == sort
            Row(
                Modifier.clip(CircleShape).background(if (on) Color.White else Color.Transparent).clickable { onSort(s) }.padding(horizontal = 14.dp, vertical = 7.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(if (s == ReelSort.Shuffle) Icons.Filled.Shuffle else Icons.Filled.AutoAwesome, null, tint = if (on) Color.Black else Color.White, modifier = Modifier.size(15.dp))
                Spacer(Modifier.width(6.dp))
                Text(s.label, color = if (on) Color.Black else Color.White, style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}
