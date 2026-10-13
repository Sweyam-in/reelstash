package app.reelstash.android.ui.home

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.common.Eyebrow
import app.reelstash.android.ui.common.GradientButton
import app.reelstash.android.ui.common.MediaArt
import app.reelstash.android.ui.common.PillButton
import app.reelstash.android.ui.common.displayTitle
import app.reelstash.android.ui.layout.LocalFormFactor
import app.reelstash.android.ui.layout.gutter
import app.reelstash.android.ui.layout.rsFocusTarget
import app.reelstash.android.ui.theme.BrandGradient
import app.reelstash.android.ui.theme.Rs
import app.reelstash.android.ui.theme.toneFor
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.MediaItem
import androidx.compose.foundation.shape.RoundedCornerShape

/** How long each slide stays, as on the web. */
private const val SLIDE_MS = 7000

/**
 * The home slideshow: full-bleed artwork fading into the page, the title in
 * display type, Play and Details, and dashes that fill as each slide's time runs.
 * Swipe to move; it pauses while a finger is down.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun HeroCarousel(
    items: List<MediaItem>,
    urls: MediaUrls,
    onPlay: (MediaItem) -> Unit,
    onOpen: (MediaItem) -> Unit,
    height: Dp,
    topInset: Dp,
    eyebrow: String = "From your collection",
) {
    if (items.isEmpty()) return
    val pager = rememberPagerState { items.size }
    val progress = remember { Animatable(0f) }
    LaunchedEffect(pager.currentPage, pager.isScrollInProgress, items.size) {
        progress.snapTo(0f)
        if (items.size < 2 || pager.isScrollInProgress) return@LaunchedEffect
        progress.animateTo(1f, tween(SLIDE_MS, easing = LinearEasing))
        pager.animateScrollToPage((pager.currentPage + 1) % items.size)
    }
    val canvas = Rs.colors.canvas
    val gutter = LocalFormFactor.current.gutter

    Box(Modifier.fillMaxWidth().height(height + topInset)) {
        HorizontalPager(pager, Modifier.fillMaxSize(), key = { items[it].id }) { page ->
            val item = items[page]
            val tone = toneFor(item.id)
            Box(Modifier.fillMaxSize()) {
                val art = if (item.backdropImageId != null) urls.artwork(item.id, MediaUrls.Artwork.Backdrop, item.backdropImageId)
                else urls.artwork(item.id, MediaUrls.Artwork.Thumbnail)
                MediaArt(item, art, Modifier.fillMaxSize(), glyphSize = 220.dp)
                Box(
                    Modifier.fillMaxSize().background(
                        Brush.verticalGradient(0f to canvas.copy(alpha = .55f), .22f to Color.Transparent, .5f to canvas.copy(alpha = .35f), .78f to canvas.copy(alpha = .92f), 1f to canvas),
                    ),
                )
                Column(Modifier.align(Alignment.BottomStart).padding(start = gutter, end = gutter, bottom = 34.dp).widthIn(max = 720.dp)) {
                    Eyebrow(eyebrow, Rs.colors.toneInk(tone.color))
                    Spacer(Modifier.height(10.dp))
                    Text(displayTitle(item), style = MaterialTheme.typography.displaySmall, color = Color.White, maxLines = 2, overflow = TextOverflow.Ellipsis)
                    val meta = listOfNotNull(item.studioName, item.seriesName, item.performers.take(2).joinToString(", ").ifBlank { null })
                        .joinToString("  ·  ").ifBlank { "From your collection" }
                    Spacer(Modifier.height(6.dp))
                    Text(meta, style = MaterialTheme.typography.bodyMedium, color = Color.White.copy(alpha = .72f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Spacer(Modifier.height(16.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        GradientButton("Play", onClick = { onPlay(item) }, icon = Icons.Filled.PlayArrow, modifier = Modifier.rsFocusTarget(RoundedCornerShape(50)))
                        PillButton("Details", onClick = { onOpen(item) }, icon = Icons.AutoMirrored.Filled.ArrowForward, modifier = Modifier.rsFocusTarget(RoundedCornerShape(50)))
                    }
                }
            }
        }
        if (items.size > 1) {
            Row(Modifier.align(Alignment.BottomStart).padding(start = gutter, bottom = 14.dp), horizontalArrangement = Arrangement.spacedBy(5.dp)) {
                items.indices.forEach { i ->
                    Box(Modifier.width(if (i == pager.currentPage) 30.dp else 18.dp).height(3.dp).clip(CircleShape).background(Color.White.copy(alpha = .22f))) {
                        val fill = when {
                            i == pager.currentPage -> progress.value
                            else -> 0f
                        }
                        if (fill > 0f) Box(Modifier.fillMaxHeight().fillMaxWidth(fill).background(BrandGradient))
                    }
                }
            }
        }
    }
}
