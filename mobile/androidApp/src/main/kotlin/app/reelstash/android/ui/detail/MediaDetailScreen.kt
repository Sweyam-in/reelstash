package app.reelstash.android.ui.detail

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Bookmark
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Star
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.StarOutline
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import app.reelstash.android.ui.common.CardShape
import app.reelstash.android.ui.common.Eyebrow
import app.reelstash.android.ui.common.GradientButton
import app.reelstash.android.ui.common.Load
import app.reelstash.android.ui.common.LoadContent
import app.reelstash.android.ui.common.MediaArt
import app.reelstash.android.ui.common.MediaRow
import app.reelstash.android.ui.common.PersonChip
import app.reelstash.android.ui.common.PillButton
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.SectionHeader
import app.reelstash.android.ui.common.ambientGlow
import app.reelstash.android.ui.common.displayTitle
import app.reelstash.android.ui.common.formatClock
import app.reelstash.android.ui.common.formatDuration
import app.reelstash.android.ui.common.rememberLoad
import app.reelstash.android.ui.layout.LocalFormFactor
import app.reelstash.android.ui.layout.gutter
import app.reelstash.android.ui.layout.isWide
import app.reelstash.android.ui.layout.rsFocusTarget
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.BrandGradient
import app.reelstash.android.ui.theme.BrandOrange
import app.reelstash.android.ui.theme.EyebrowStyle
import app.reelstash.android.ui.theme.Rs
import app.reelstash.android.ui.theme.Tone
import app.reelstash.android.ui.theme.Tones
import app.reelstash.android.ui.theme.sectionTone
import app.reelstash.android.ui.theme.toneFor
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.api.ReelStashClient
import app.reelstash.shared.model.MediaItem
import kotlinx.coroutines.launch

/** How many suggestions to show under a title. */
private const val SUGGESTIONS = 12

private val StarGold = Color(0xFFF5C330)

/**
 * Titles to suggest after this one. The server matches on people, studio,
 * series, categories and tags and tops up from the same library; older servers
 * only match people and categories, so an unmatched video could get nothing —
 * then recent titles fill in here.
 */
private suspend fun suggestions(client: ReelStashClient, id: Long): List<MediaItem> {
    val related = runCatching { client.related(id).items }.getOrDefault(emptyList())
    if (related.size >= SUGGESTIONS) return related
    val seen = related.map { it.id }.toMutableSet().apply { add(id) }
    val more = runCatching { client.media(MediaQuery(), limit = SUGGESTIONS * 2).items }.getOrDefault(emptyList())
        .filter { seen.add(it.id) }
        .sortedBy { it.watched }
        .map { if (it.reason == null) it.copy(reason = "Recently added") else it }
    return related + more.take(SUGGESTIONS - related.size)
}

@Composable
fun MediaDetailScreen(id: Long, nav: Navigator) {
    val server = LocalServer.current
    val detail = rememberLoad("media:${server.id}:$id") { server.client.mediaItem(id) }
    val related = rememberLoad("related:${server.id}:$id") { suggestions(server.client, id) }

    // Back from the player: pick up the new resume point.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) {
        var first = true
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            if (!first) {
                kotlinx.coroutines.delay(400)
                detail.refreshQuietly()
            }
            first = false
        }
    }

    val form = LocalFormFactor.current
    val gutter = form.gutter
    Column(Modifier.fillMaxSize().ambientGlow(toneFor(id).color, .5f)) {
        RsTopBar(nav, onBack = { nav.back() }) {}
        LoadContent(detail.state, detail::retry) { item ->
            val tone = toneFor(item.id)
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Column(Modifier.widthIn(max = if (form.isWide) 1100.dp else 880.dp).padding(horizontal = gutter)) {
                    if (form.isWide) {
                        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(28.dp)) {
                            Artwork(item, server.client.urls, Modifier.weight(1.15f)) { nav.play(item.id) }
                            Column(Modifier.weight(1f).padding(top = 8.dp)) {
                                DetailHeader(item, tone, nav)
                                Spacer(Modifier.height(20.dp))
                                Rating(item, onChange = detail.vm::set)
                                Spacer(Modifier.height(18.dp))
                                Actions(item, nav, onChange = detail.vm::set)
                                item.description?.takeIf { it.isNotBlank() }?.let { Description(it) }
                            }
                        }
                        Spacer(Modifier.height(26.dp))
                        HorizontalDivider(color = Rs.colors.border)
                        Spacer(Modifier.height(24.dp))
                        Facts(item, nav)
                    } else {
                        Artwork(item, server.client.urls) { nav.play(item.id) }
                        Spacer(Modifier.height(22.dp))
                        DetailHeader(item, tone, nav)
                        Spacer(Modifier.height(20.dp))
                        Rating(item, onChange = detail.vm::set)
                        Spacer(Modifier.height(18.dp))
                        Actions(item, nav, onChange = detail.vm::set)
                        item.description?.takeIf { it.isNotBlank() }?.let { Description(it) }
                        Spacer(Modifier.height(26.dp))
                        HorizontalDivider(color = Rs.colors.border)
                        Spacer(Modifier.height(24.dp))
                        Facts(item, nav)
                    }
                }
                if (item.cast.isNotEmpty() || item.crew.isNotEmpty()) {
                    Spacer(Modifier.height(26.dp))
                    SectionHeader("Cast & crew", "The people", Tones[7])
                    Spacer(Modifier.height(12.dp))
                    LazyRow(contentPadding = PaddingValues(horizontal = gutter), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        items(item.cast + item.crew, key = { "${it.id}:${it.job}" }) { p -> PersonChip(p, server.client.urls) { nav.openPerson(p.id) } }
                    }
                }
                val more = (related.state as? Load.Ready)?.value.orEmpty()
                MediaRow(
                    if (more.any { it.reason == null }) "More like this" else "You might also like", more, server.client.urls, CardShape.Landscape, { nav.openMedia(it.id) },
                    eyebrow = "Suggested", tone = sectionTone(2),
                )
                Spacer(Modifier.height(32.dp))
            }
        }
    }
}

@Composable
private fun DetailHeader(item: MediaItem, tone: Tone, nav: Navigator) {
    (item.studioName ?: item.seriesName)?.let {
        Eyebrow(it, Rs.colors.toneInk(tone.color), dot = false, modifier = Modifier.clickable {
            if (item.studioName == null) item.seriesId?.let(nav::openSeries)
        })
        Spacer(Modifier.height(8.dp))
    }
    Text(displayTitle(item), style = MaterialTheme.typography.headlineLarge)
    val meta = listOfNotNull(
        item.episodeLabel,
        item.year?.toString(),
        formatDuration(item.durationSeconds ?: item.files.firstOrNull()?.durationSeconds),
        resolution(item),
    ).joinToString("  ·  ")
    if (meta.isNotEmpty()) {
        Spacer(Modifier.height(6.dp))
        Text(meta, style = MaterialTheme.typography.bodyMedium, color = Rs.colors.textSecondary)
    }
}

private fun resolution(item: MediaItem): String? {
    val h = item.files.firstOrNull { it.available }?.height ?: return null
    return when {
        h >= 2000 -> "4K"
        h >= 1000 -> "1080p"
        h >= 700 -> "720p"
        else -> "${h}p"
    }
}

/** The artwork card at the top, with a play mark over it. */
@Composable
private fun Artwork(item: MediaItem, urls: MediaUrls, modifier: Modifier = Modifier, onPlay: () -> Unit) {
    val art = if (item.backdropImageId != null) urls.artwork(item.id, MediaUrls.Artwork.Backdrop, item.backdropImageId) else urls.artwork(item.id, MediaUrls.Artwork.Thumbnail)
    Box(
        modifier.fillMaxWidth().aspectRatio(16f / 10f)
            .rsFocusTarget(RoundedCornerShape(18.dp))
            .clip(RoundedCornerShape(18.dp))
            .border(1.dp, Color.White.copy(alpha = .08f), RoundedCornerShape(18.dp))
            .clickable(enabled = item.playable, onClick = onPlay),
    ) {
        MediaArt(item, art, Modifier.fillMaxSize(), glyphSize = 150.dp)
        if (item.playable) {
            Box(
                Modifier.align(Alignment.Center).size(64.dp).clip(CircleShape).background(Color.Black.copy(alpha = .45f))
                    .border(1.dp, Color.White.copy(alpha = .3f), CircleShape),
                contentAlignment = Alignment.Center,
            ) { Icon(Icons.Filled.PlayArrow, "Play", tint = Color.White, modifier = Modifier.size(34.dp)) }
        } else {
            Text(
                "This file isn't available right now.", color = Color.White, style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.align(Alignment.Center).clip(RoundedCornerShape(12.dp)).background(Color(0xE6101320)).padding(horizontal = 16.dp, vertical = 12.dp),
            )
        }
        item.progress?.let { p ->
            Box(Modifier.align(Alignment.BottomStart).fillMaxWidth().height(4.dp).background(Color.White.copy(alpha = .22f))) {
                Box(Modifier.fillMaxHeight().fillMaxWidth(p).background(BrandGradient))
            }
        }
    }
}

/** Your 0–5 star rating, stored server-side as 0–10. Tapping the current star clears it. */
@Composable
private fun Rating(item: MediaItem, onChange: (MediaItem) -> Unit) {
    val client = LocalServer.current.client
    val scope = rememberCoroutineScope()
    val stars = ((item.myRating ?: 0) + 1) / 2
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text("YOUR RATING", style = EyebrowStyle, color = Rs.colors.textTertiary)
        Spacer(Modifier.size(12.dp))
        (1..5).forEach { n ->
            val on = n <= stars
            Icon(
                if (on) Icons.Filled.Star else Icons.Outlined.StarOutline, "$n stars",
                tint = if (on) StarGold else StarGold.copy(alpha = .55f),
                modifier = Modifier.size(34.dp).clip(CircleShape).clickable {
                    val next = if (n == stars) 0 else n
                    val before = item
                    onChange(item.copy(myRating = next * 2))
                    scope.launch { runCatching { client.setRating(item.id, next * 2) }.onFailure { onChange(before) } }
                }.padding(3.dp),
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Actions(item: MediaItem, nav: Navigator, onChange: (MediaItem) -> Unit) {
    val client = LocalServer.current.client
    val scope = rememberCoroutineScope()
    // Optimistic toggles: flip at once, put back if the server refuses.
    fun toggle(changed: MediaItem, call: suspend () -> Unit) {
        val before = item
        onChange(changed)
        scope.launch { runCatching { call() }.onFailure { onChange(before) } }
    }

    val resume = item.progress
    FlowRow(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        GradientButton(
            when {
                !item.playable -> "File offline"
                resume != null -> "Resume from ${formatClock(item.positionSeconds)}"
                else -> "Play"
            },
            onClick = { nav.play(item.id) }, icon = Icons.Filled.PlayArrow, enabled = item.playable,
        )
        PillButton(
            "Favorite", onClick = { toggle(item.copy(favorite = !item.favorite)) { client.setFavorite(item.id, !item.favorite) } },
            icon = if (item.favorite) Icons.Filled.Favorite else Icons.Outlined.FavoriteBorder, iconTint = if (item.favorite) BrandOrange else Rs.colors.text, selected = item.favorite,
        )
        PillButton(
            "Watch later", onClick = { toggle(item.copy(watchLater = !item.watchLater)) { client.setWatchLater(item.id, !item.watchLater) } },
            icon = if (item.watchLater) Icons.Filled.Bookmark else Icons.Outlined.BookmarkBorder, selected = item.watchLater,
        )
        PillButton(
            if (item.watched) "Watched" else "Mark watched",
            onClick = { toggle(item.copy(watched = !item.watched, positionSeconds = if (!item.watched) null else item.positionSeconds)) { client.setWatched(item.id, !item.watched) } },
            icon = if (item.watched) Icons.Filled.CheckCircle else Icons.Outlined.CheckCircle, selected = item.watched,
        )
    }
}

@Composable
private fun Description(text: String) {
    var expanded by rememberSaveable { mutableStateOf(false) }
    Text(
        text,
        style = MaterialTheme.typography.bodyMedium,
        color = Rs.colors.textSecondary,
        maxLines = if (expanded) Int.MAX_VALUE else 4,
        overflow = TextOverflow.Ellipsis,
        modifier = Modifier.padding(top = 20.dp).animateContentSize().clickable { expanded = !expanded },
    )
}

private data class Fact(val label: String, val value: String, val onClick: (() -> Unit)? = null)

/** "About this title": the facts, as the web lays them out. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun Facts(item: MediaItem, nav: Navigator) {
    Eyebrow("The details", Rs.colors.toneInk(Tones[7]), dot = false)
    Spacer(Modifier.height(10.dp))
    Text("About this title", style = MaterialTheme.typography.headlineSmall)
    Spacer(Modifier.height(16.dp))
    val file = item.files.firstOrNull { it.available } ?: item.files.firstOrNull()
    val video = file?.let { f ->
        listOfNotNull(f.width?.let { w -> f.height?.let { h -> "$w×$h" } }, f.videoCodec?.uppercase(), f.audioCodec?.uppercase()).joinToString(" · ").ifBlank { null }
    }
    val facts = listOfNotNull(
        item.studioName?.let { Fact("Studio", it) },
        item.seriesName?.let { name -> Fact("Series", name, item.seriesId?.let { id -> { nav.openSeries(id) } }) },
        item.releaseDate?.take(10)?.let { Fact("Released", it) } ?: item.year?.let { Fact("Year", it.toString()) },
        formatDuration(item.durationSeconds ?: file?.durationSeconds)?.let { Fact("Runtime", it) },
        video?.let { Fact("Video", it) },
        item.rating?.let { Fact("Average rating", "★ %.1f".format(it / 2.0)) },
    )
    FlowRow(horizontalArrangement = Arrangement.spacedBy(28.dp), verticalArrangement = Arrangement.spacedBy(18.dp)) {
        facts.forEach { f ->
            Column(Modifier.widthIn(min = 120.dp).then(if (f.onClick != null) Modifier.clickable(onClick = f.onClick) else Modifier)) {
                Text(f.label.uppercase(), style = EyebrowStyle, color = Rs.colors.textTertiary)
                Spacer(Modifier.height(5.dp))
                Text(f.value, style = MaterialTheme.typography.titleMedium, color = if (f.onClick != null) Rs.colors.accentText else Rs.colors.text)
            }
        }
    }
    val labels = (item.categories + item.tags).distinct()
    if (labels.isNotEmpty()) {
        Spacer(Modifier.height(20.dp))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            labels.take(16).forEach { label ->
                val t = toneFor(label)
                Text(
                    label, style = MaterialTheme.typography.labelMedium, color = Rs.colors.toneInk(t.color),
                    modifier = Modifier.clip(CircleShape).background(t.color.copy(alpha = .13f)).border(1.dp, t.color.copy(alpha = .25f), CircleShape)
                        .padding(horizontal = 12.dp, vertical = 6.dp),
                )
            }
        }
    }
}
