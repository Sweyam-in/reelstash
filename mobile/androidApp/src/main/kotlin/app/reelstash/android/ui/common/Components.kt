package app.reelstash.android.ui.common

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowForward
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.reelstash.android.ui.theme.BrandGradient
import app.reelstash.android.ui.theme.BrandOrange
import app.reelstash.android.ui.theme.DisplayFamily
import app.reelstash.android.ui.theme.EyebrowStyle
import app.reelstash.android.ui.theme.KickerStyle
import app.reelstash.android.ui.theme.Rs
import app.reelstash.android.ui.theme.Tone
import app.reelstash.android.ui.theme.LocalPalette
import app.reelstash.android.ui.theme.toneFor
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.Person
import coil3.compose.SubcomposeAsyncImage
import java.time.Duration
import java.time.OffsetDateTime

// ---- States -------------------------------------------------------------------

@Composable
fun LoadingBox(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(color = Rs.colors.accent, strokeWidth = 3.dp)
    }
}

@Composable
fun MessageBox(
    title: String,
    message: String? = null,
    modifier: Modifier = Modifier,
    actionLabel: String? = null,
    icon: ImageVector = Icons.Outlined.CloudOff,
    onAction: (() -> Unit)? = null,
) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier.size(56.dp).clip(CircleShape).background(Rs.colors.accent.copy(alpha = .1f))
                .border(1.dp, Rs.colors.accent.copy(alpha = .3f), CircleShape),
            contentAlignment = Alignment.Center,
        ) { Icon(icon, null, tint = Rs.colors.accentText, modifier = Modifier.size(24.dp)) }
        Spacer(Modifier.height(16.dp))
        Text(title, style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
        message?.let {
            Spacer(Modifier.height(6.dp))
            Text(it, style = MaterialTheme.typography.bodyMedium, color = Rs.colors.textSecondary, textAlign = TextAlign.Center)
        }
        if (actionLabel != null && onAction != null) {
            Spacer(Modifier.height(20.dp))
            GradientButton(actionLabel, onClick = onAction)
        }
    }
}

/** Renders a [Load]: spinner, error with retry, or the content. */
@Composable
fun <T> LoadContent(state: Load<T>, onRetry: () -> Unit, modifier: Modifier = Modifier, content: @Composable (T) -> Unit) {
    when (state) {
        Load.Loading -> LoadingBox(modifier)
        is Load.Failed -> MessageBox("Couldn't load this", state.message, modifier, "Try again", onAction = onRetry)
        is Load.Ready -> content(state.value)
    }
}

// ---- Type and buttons ------------------------------------------------------------

/** The small all-caps label with a coloured dot that sits above headings. */
@Composable
fun Eyebrow(text: String, color: Color = Rs.colors.accentText, modifier: Modifier = Modifier, dot: Boolean = true) {
    Row(modifier, verticalAlignment = Alignment.CenterVertically) {
        if (dot) {
            Box(Modifier.size(7.dp).clip(CircleShape).background(color))
            Spacer(Modifier.width(7.dp))
        }
        Text(text.uppercase(), style = EyebrowStyle, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** The brand call to action: a pill in the orange→green gradient, with a soft orange glow. */
@Composable
fun GradientButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    enabled: Boolean = true,
    height: Dp = 44.dp,
) {
    Row(
        modifier
            .height(height)
            .shadow(if (enabled) 14.dp else 0.dp, CircleShape, ambientColor = BrandOrange, spotColor = BrandOrange)
            .clip(CircleShape)
            .background(BrandGradient, alpha = if (enabled) 1f else .45f)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 20.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        icon?.let {
            Icon(it, null, tint = Color.White, modifier = Modifier.size(19.dp))
            Spacer(Modifier.width(8.dp))
        }
        Text(text, color = Color.White.copy(alpha = if (enabled) 1f else .7f), style = MaterialTheme.typography.labelLarge, maxLines = 1)
    }
}

/** A secondary action: an outlined pill on the surface colour, like the web's outline buttons. */
@Composable
fun PillButton(
    text: String?,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    icon: ImageVector? = null,
    iconTint: Color = Rs.colors.text,
    selected: Boolean = false,
    height: Dp = 44.dp,
) {
    val c = Rs.colors
    Row(
        modifier
            .height(height)
            .clip(CircleShape)
            .background(if (selected) c.accent.copy(alpha = .14f) else c.surface)
            .border(BorderStroke(1.dp, if (selected) c.accent.copy(alpha = .45f) else c.borderStrong), CircleShape)
            .clickable(onClick = onClick)
            .padding(horizontal = if (text == null) 12.dp else 16.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        icon?.let { Icon(it, null, tint = iconTint, modifier = Modifier.size(19.dp)) }
        if (icon != null && text != null) Spacer(Modifier.width(8.dp))
        text?.let { Text(it, style = MaterialTheme.typography.labelLarge, color = c.text, maxLines = 1) }
    }
}

/** A section's header: eyebrow, display title, and an optional "View all" link in the section's tone. */
@Composable
fun SectionHeader(
    title: String,
    eyebrow: String? = null,
    tone: Color? = null,
    modifier: Modifier = Modifier,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
) {
    val ink = tone?.let { Rs.colors.toneInk(it) } ?: Rs.colors.accentText
    Row(modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.Bottom) {
        Column(Modifier.weight(1f)) {
            eyebrow?.let {
                Eyebrow(it, ink)
                Spacer(Modifier.height(6.dp))
            }
            Text(title, style = MaterialTheme.typography.headlineSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        if (actionLabel != null && onAction != null) {
            Row(Modifier.clip(CircleShape).clickable(onClick = onAction).padding(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(actionLabel, color = ink, style = MaterialTheme.typography.labelLarge)
                Spacer(Modifier.width(4.dp))
                Icon(Icons.AutoMirrored.Filled.ArrowForward, null, tint = ink, modifier = Modifier.size(16.dp))
            }
        }
    }
}

// ---- Artwork --------------------------------------------------------------------

/** The title as shown: without a leading "Studio: " the studio already says. */
fun displayTitle(item: MediaItem): String {
    val studio = item.studioName
    return if (studio != null && item.title.lowercase().startsWith(studio.lowercase() + ": ")) item.title.substring(studio.length + 2) else item.title
}

/** Up to two initials for artwork fallbacks ("After Hours" → "AH", "scene-100" → "S1"). */
fun initials(title: String): String {
    val words = title.replace(Regex("[^\\p{L}\\p{N}\\s-]"), "").split(Regex("[\\s-]+")).filter { it.isNotBlank() }
    return when {
        words.size > 1 -> "${words[0][0]}${words[1][0]}"
        words.isNotEmpty() -> words[0].take(2)
        else -> "?"
    }.uppercase()
}

/**
 * A colour-field "poster" for titles without artwork, as the web draws it: the
 * title's tone washed into its partner, fine diagonal lines, big initials.
 */
@Composable
fun ToneArt(key: Any, title: String, label: String?, modifier: Modifier = Modifier, glyphSize: Dp = 64.dp) {
    val tone = toneFor(key)
    Box(
        modifier
            .background(Brush.linearGradient(listOf(tone.color, lerpDark(tone.color))))
            .drawBehind {
                drawRect(Brush.radialGradient(listOf(tone.partner.copy(alpha = .8f), Color.Transparent), center = Offset(size.width, 0f), radius = size.maxDimension * .7f))
                drawRect(Brush.verticalGradient(listOf(Color.White.copy(alpha = .12f), Color.Transparent), endY = size.height * .45f))
                val step = 14.dp.toPx()
                var x = -size.height
                while (x < size.width) {
                    drawLine(Color.White.copy(alpha = .06f), Offset(x, size.height), Offset(x + size.height * .47f, 0f), strokeWidth = 1f)
                    x += step
                }
            },
    ) {
        Text(
            initials(title),
            fontFamily = DisplayFamily,
            fontWeight = FontWeight.ExtraBold,
            fontSize = with(androidx.compose.ui.platform.LocalDensity.current) { glyphSize.toSp() },
            letterSpacing = (-3).sp,
            color = Color.White.copy(alpha = .55f),
            maxLines = 1,
            modifier = Modifier.align(Alignment.BottomEnd).padding(end = 2.dp),
        )
        label?.let {
            Text(
                it.uppercase(), style = EyebrowStyle, color = Color.White.copy(alpha = .88f), maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.align(Alignment.BottomStart).padding(start = 12.dp, bottom = 12.dp).fillMaxWidth(.6f),
            )
        }
    }
}

private fun lerpDark(c: Color) = androidx.compose.ui.graphics.lerp(c, Color(0xFF1B1030), .45f)

/** A server image over a fallback; the fallback shows while loading and when there is no image. */
@Composable
fun ServerImage(url: String?, modifier: Modifier = Modifier, fallback: @Composable BoxScope.() -> Unit = {}) {
    Box(modifier) {
        var loaded by remember(url) { mutableStateOf(false) }
        if (!loaded) fallback()
        if (url != null) {
            SubcomposeAsyncImage(
                model = url,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
                onSuccess = { loaded = true },
                onError = { loaded = false },
            )
        }
    }
}

/** A title's artwork, falling back to its tone art. */
@Composable
fun MediaArt(item: MediaItem, url: String?, modifier: Modifier = Modifier, glyphSize: Dp = 64.dp) {
    ServerImage(url, modifier) {
        ToneArt(item.id, displayTitle(item), item.studioName ?: "ReelStash", Modifier.fillMaxSize(), glyphSize)
    }
}

@Composable
fun ImagePlaceholder(icon: ImageVector) {
    Box(Modifier.fillMaxSize().background(Rs.colors.raised), contentAlignment = Alignment.Center) {
        Icon(icon, null, tint = Rs.colors.textTertiary, modifier = Modifier.size(28.dp))
    }
}

// ---- Cards ------------------------------------------------------------------------

/** Shape a title card takes: posters for movies and shows, landscape thumbnails for everything else. */
enum class CardShape(val ratio: Float, val artwork: MediaUrls.Artwork) {
    Poster(2f / 3f, MediaUrls.Artwork.Poster),
    Landscape(16f / 10f, MediaUrls.Artwork.Thumbnail),
}

private val NEW_FOR: Duration = Duration.ofDays(14)

fun isNew(item: MediaItem): Boolean =
    item.dateAdded?.let { runCatching { Duration.between(OffsetDateTime.parse(it), OffsetDateTime.now()) < NEW_FOR }.getOrNull() } == true

/** The coloured line above a card's title: why it is suggested, else where it comes from. */
fun kickerFor(item: MediaItem): String? = item.reason ?: item.episodeLabel ?: item.seriesName ?: item.studioName ?: item.year?.toString()

@Composable
fun MediaCard(
    item: MediaItem,
    urls: MediaUrls,
    shape: CardShape,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    kicker: String? = kickerFor(item),
    tone: Tone? = null,
) {
    val c = Rs.colors
    val toneColor = tone ?: toneFor(item.id)
    Column(modifier.clip(RoundedCornerShape(14.dp)).clickable(onClick = onClick)) {
        Box(Modifier.fillMaxWidth().aspectRatio(shape.ratio).clip(RoundedCornerShape(14.dp))) {
            MediaArt(item, urls.artwork(item.id, shape.artwork, if (shape == CardShape.Poster) item.posterImageId else null), Modifier.fillMaxSize(), glyphSize = if (shape == CardShape.Poster) 56.dp else 64.dp)
            if (isNew(item)) {
                Text(
                    "NEW", style = EyebrowStyle.copy(fontSize = 9.sp), color = Color.White,
                    modifier = Modifier.align(Alignment.TopStart).padding(8.dp).clip(CircleShape).background(BrandGradient).padding(horizontal = 8.dp, vertical = 3.dp),
                )
            }
            Row(Modifier.align(Alignment.TopEnd).padding(8.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (item.watched) Badge(Icons.Filled.Check, Color.White)
                if (item.favorite) Badge(Icons.Filled.Favorite, BrandOrange)
            }
            item.progress?.let { p ->
                Box(Modifier.align(Alignment.BottomStart).fillMaxWidth().height(4.dp).background(Color.White.copy(alpha = .22f))) {
                    Box(Modifier.fillMaxWidth(p).fillMaxHeight().clip(RoundedCornerShape(topEnd = 4.dp, bottomEnd = 4.dp)).background(BrandGradient))
                }
            }
        }
        Spacer(Modifier.height(8.dp))
        kicker?.let { Text(it, style = KickerStyle, color = c.toneInk(toneColor.color), maxLines = 1, overflow = TextOverflow.Ellipsis) }
        Text(displayTitle(item), style = MaterialTheme.typography.titleSmall, color = c.text, maxLines = 2, overflow = TextOverflow.Ellipsis)
        if (item.performers.isNotEmpty()) {
            Text(item.performers.take(2).joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = c.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun Badge(icon: ImageVector, tint: Color) {
    Box(Modifier.size(24.dp).clip(CircleShape).background(Color.Black.copy(alpha = .55f)), contentAlignment = Alignment.Center) {
        Icon(icon, null, tint = tint, modifier = Modifier.size(14.dp))
    }
}

/** A home "shelf": header plus a horizontal row of cards, all in the section's tone. */
@Composable
fun MediaRow(
    title: String,
    items: List<MediaItem>,
    urls: MediaUrls,
    shape: CardShape,
    onOpen: (MediaItem) -> Unit,
    eyebrow: String? = null,
    tone: Tone? = null,
    cardWidth: Dp = if (shape == CardShape.Poster) 132.dp else 232.dp,
    onViewAll: (() -> Unit)? = null,
    kicker: (MediaItem) -> String? = { kickerFor(it) },
) {
    if (items.isEmpty()) return
    Column(Modifier.padding(vertical = 14.dp)) {
        SectionHeader(title, eyebrow, tone?.color, actionLabel = if (onViewAll != null) "View all" else null, onAction = onViewAll)
        Spacer(Modifier.height(14.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            items(items, key = { it.id }) { item ->
                MediaCard(item, urls, shape, onClick = { onOpen(item) }, modifier = Modifier.width(cardWidth), kicker = kicker(item), tone = tone ?: toneFor(item.id))
            }
        }
    }
}

@Composable
fun PersonChip(person: Person, urls: MediaUrls, onClick: () -> Unit) {
    val tone = toneFor("p${person.id}")
    Column(Modifier.width(88.dp).clip(RoundedCornerShape(12.dp)).clickable(onClick = onClick).padding(vertical = 4.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.size(76.dp).clip(CircleShape).border(2.dp, tone.color.copy(alpha = .55f), CircleShape).padding(3.dp).clip(CircleShape)) {
            ServerImage(person.profileImageId?.let { urls.performerImage(person.id, it) }, Modifier.fillMaxSize()) {
                ToneArt("p${person.id}", person.name, null, Modifier.fillMaxSize(), glyphSize = 30.dp)
            }
        }
        Spacer(Modifier.height(8.dp))
        Text(person.name, style = MaterialTheme.typography.labelMedium, maxLines = 2, textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis)
        (person.character ?: person.job)?.let {
            Text(it, style = MaterialTheme.typography.labelSmall, color = Rs.colors.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center)
        }
    }
}

/** The faint two-colour wash the web puts behind the top of each page. */
@Composable
fun Modifier.ambientGlow(color: Color, strength: Float = .55f): Modifier {
    val partner = LocalPalette.current.to
    return drawBehind {
        drawRect(Brush.radialGradient(listOf(color.copy(alpha = strength * .6f), Color.Transparent), center = Offset(size.width * .35f, 0f), radius = size.width * .95f))
        drawRect(Brush.radialGradient(listOf(partner.copy(alpha = strength * .25f), Color.Transparent), center = Offset(size.width, size.height * .1f), radius = size.width * .7f))
    }
}

/** Darkens the bottom of artwork so text over it stays readable. */
fun scrim(bottom: Color) = Brush.verticalGradient(0f to Color.Transparent, 0.5f to bottom.copy(alpha = .45f), 1f to bottom)

fun formatDuration(seconds: Double?): String? {
    val s = seconds?.toLong()?.takeIf { it > 0 } ?: return null
    val h = s / 3600
    val m = (s % 3600) / 60
    return when {
        h > 0 -> "${h}h ${m.toString().padStart(2, '0')}m"
        m > 0 -> "${m}m"
        else -> "${s}s"
    }
}

/** A position as a clock: 4:05, or 1:02:09 past an hour. */
fun formatClock(seconds: Double?): String {
    val s = (seconds ?: 0.0).toLong().coerceAtLeast(0)
    val h = s / 3600
    val m = (s % 3600) / 60
    val sec = s % 60
    return if (h > 0) "%d:%02d:%02d".format(h, m, sec) else "%d:%02d".format(m, sec)
}
