package app.reelstash.android.ui.browse

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridScope
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Tv
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.reelstash.android.ui.common.CardShape
import app.reelstash.android.ui.common.ToneArt
import app.reelstash.android.ui.common.LoadingBox
import app.reelstash.android.ui.common.MediaCard
import app.reelstash.android.ui.common.MessageBox
import app.reelstash.android.ui.common.PagedViewModel
import app.reelstash.android.ui.common.ServerImage
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.Performer
import app.reelstash.shared.model.Series

/**
 * An endless grid over a [PagedViewModel]. Columns adapt to the width, so a
 * tablet shows more of them; the next page loads as the end comes into view.
 */
@Composable
fun <T> PagedGrid(
    paged: PagedViewModel<T>,
    minCellWidth: androidx.compose.ui.unit.Dp,
    emptyTitle: String,
    modifier: Modifier = Modifier,
    header: (LazyGridScope.() -> Unit)? = null,
    key: (T) -> Any,
    cell: @Composable (T) -> Unit,
) {
    val state by paged.state.collectAsStateWithLifecycle()
    val grid = rememberLazyGridState()
    val nearEnd by remember {
        derivedStateOf {
            val info = grid.layoutInfo
            val last = info.visibleItemsInfo.lastOrNull()?.index ?: 0
            last >= info.totalItemsCount - 12
        }
    }
    LaunchedEffect(nearEnd, state.items.size) { if (nearEnd) paged.loadMore() }

    when {
        state.initialLoading -> LoadingBox(modifier)
        state.items.isEmpty() && state.error != null -> MessageBox("Couldn't load this", state.error, modifier, "Try again") { paged.reload() }
        state.items.isEmpty() && state.endReached -> MessageBox(emptyTitle, modifier = modifier)
        else -> LazyVerticalGrid(
            columns = GridCells.Adaptive(minCellWidth),
            state = grid,
            modifier = modifier.fillMaxSize(),
            contentPadding = PaddingValues(16.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            header?.invoke(this)
            items(state.items, key = key) { cell(it) }
            if (state.loading || state.error != null) {
                item(span = { GridItemSpan(maxLineSpan) }) {
                    Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) {
                        if (state.error != null) TextButton(onClick = paged::loadMore) { Text("Couldn't load more. Try again") }
                        else CircularProgressIndicator()
                    }
                }
            }
        }
    }
}

@Composable
fun MediaGrid(paged: PagedViewModel<MediaItem>, urls: MediaUrls, shape: CardShape, emptyTitle: String, onOpen: (MediaItem) -> Unit, modifier: Modifier = Modifier, header: (LazyGridScope.() -> Unit)? = null) {
    PagedGrid(paged, if (shape == CardShape.Poster) 110.dp else 170.dp, emptyTitle, modifier, header, key = { it.id }) { item ->
        MediaCard(item, urls, shape, onClick = { onOpen(item) })
    }
}

@Composable
fun SeriesCard(series: Series, urls: MediaUrls, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick)) {
        Box(Modifier.fillMaxWidth().aspectRatio(2f / 3f).clip(RoundedCornerShape(14.dp))) {
            ServerImage(urls.seriesImage(series.id, series.posterImageId), Modifier.fillMaxSize()) {
                ToneArt("s${series.id}", series.name, series.studioName, Modifier.fillMaxSize(), glyphSize = 56.dp)
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(series.name, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
        val sub = if (series.seasonCount > 1) "${series.seasonCount} seasons" else "${series.mediaCount} episodes"
        Text(sub, style = MaterialTheme.typography.bodySmall, color = app.reelstash.android.ui.theme.Rs.colors.textTertiary)
    }
}

@Composable
fun PerformerCard(performer: Performer, urls: MediaUrls, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick), horizontalAlignment = Alignment.CenterHorizontally) {
        Box(Modifier.fillMaxWidth().aspectRatio(1f).clip(CircleShape)) {
            ServerImage(performer.profileImageId?.let { urls.performerImage(performer.id, it) }, Modifier.fillMaxSize()) {
                ToneArt("p${performer.id}", performer.name, null, Modifier.fillMaxSize(), glyphSize = 40.dp)
            }
        }
        Spacer(Modifier.height(6.dp))
        Text(performer.name, style = MaterialTheme.typography.titleSmall, maxLines = 2, textAlign = TextAlign.Center, overflow = TextOverflow.Ellipsis)
        Text("${performer.mediaCount} titles", style = MaterialTheme.typography.bodySmall, color = app.reelstash.android.ui.theme.Rs.colors.textTertiary)
    }
}
