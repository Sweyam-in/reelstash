package app.reelstash.android.ui.detail

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.browse.MediaGrid
import app.reelstash.android.ui.common.ImagePlaceholder
import app.reelstash.android.ui.common.LoadContent
import app.reelstash.android.ui.common.MessageBox
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.TopBarTitle
import app.reelstash.android.ui.common.ServerImage
import app.reelstash.android.ui.common.cardShapeFor
import app.reelstash.android.ui.common.currentLibrary
import app.reelstash.android.ui.common.formatDuration
import app.reelstash.android.ui.common.rememberLibraries
import app.reelstash.android.ui.common.rememberLoad
import app.reelstash.android.ui.common.rememberPaged
import app.reelstash.android.ui.common.valueOrNull
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.api.MediaUrls
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.Page
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope

@Composable
private fun BackScaffold(title: String, nav: Navigator, content: @Composable (Modifier) -> Unit) {
    Column(Modifier.fillMaxSize()) {
        RsTopBar(nav, onBack = { nav.back() }) { TopBarTitle(title) }
        content(Modifier)
    }
}

/** A show or series: its episodes, by season when there is more than one. */
@Composable
fun SeriesScreen(id: Long, nav: Navigator) {
    val server = LocalServer.current
    val client = server.client
    val data = rememberLoad("series:${server.id}:$id") {
        coroutineScope {
            val series = async { client.seriesDetail(id) }
            val episodes = async { allPages { limit, offset -> client.media(MediaQuery(seriesId = id, sort = MediaQuery.Sort.Series), limit, offset) } }
            series.await() to episodes.await()
        }
    }
    BackScaffold(data.state.valueOrNull?.first?.name ?: "", nav) { modifier ->
        LoadContent(data.state, data::retry, modifier) { (series, episodes) ->
            val seasons = episodes.groupBy { it.seasonNumber ?: 0 }.toSortedMap()
            var season by rememberSaveable { mutableStateOf(seasons.keys.firstOrNull { it > 0 } ?: seasons.keys.firstOrNull() ?: 0) }
            Column(modifier.fillMaxSize()) {
                if (seasons.size > 1) {
                    PrimaryScrollableTabRow(selectedTabIndex = seasons.keys.indexOf(season).coerceAtLeast(0), edgePadding = 16.dp) {
                        seasons.keys.forEach { s -> Tab(selected = s == season, onClick = { season = s }, text = { Text(if (s == 0) "Specials" else "Season $s") }) }
                    }
                }
                LazyColumn(Modifier.fillMaxSize()) {
                    series.description?.takeIf { it.isNotBlank() }?.let { d ->
                        item { Text(d, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 5, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(16.dp)) }
                    }
                    val list = if (seasons.size > 1) seasons[season].orEmpty() else episodes
                    if (list.isEmpty()) item { MessageBox("No episodes yet") }
                    items(list, key = { it.id }) { ep -> EpisodeRow(ep, client.urls) { nav.openMedia(ep.id) } }
                }
            }
        }
    }
}

private suspend fun allPages(fetch: suspend (Int, Int) -> Page<MediaItem>): List<MediaItem> {
    val out = mutableListOf<MediaItem>()
    while (true) {
        val page = fetch(200, out.size)
        out += page.items
        if (page.items.isEmpty() || out.size >= page.total || out.size >= 2000) return out
    }
}

@Composable
private fun EpisodeRow(ep: MediaItem, urls: MediaUrls, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.width(150.dp).aspectRatio(16f / 9f).clip(RoundedCornerShape(8.dp))) {
            ServerImage(urls.artwork(ep.id, MediaUrls.Artwork.Thumbnail), Modifier.fillMaxSize())
            ep.progress?.let { p ->
                LinearProgressIndicator(progress = { p }, modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(3.dp), trackColor = Color.Black.copy(alpha = .5f), drawStopIndicator = {}, gapSize = 0.dp)
            }
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            ep.episodeLabel?.let { Text(it, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.primary) }
            Text(ep.title, style = MaterialTheme.typography.bodyLarge, maxLines = 2, overflow = TextOverflow.Ellipsis)
            val meta = listOfNotNull(formatDuration(ep.durationSeconds), if (ep.watched) "Watched" else null).joinToString(" · ")
            if (meta.isNotEmpty()) Text(meta, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
fun PlaylistScreen(id: Long, nav: Navigator) {
    val server = LocalServer.current
    val library = currentLibrary(rememberLibraries().state.valueOrNull)
    var title by rememberSaveable { mutableStateOf("") }
    val paged = rememberPaged("playlist:${server.id}:$id") { limit, offset ->
        val detail = server.client.playlist(id, limit, offset)
        title = detail.collection.name
        Page(detail.items, detail.limit, detail.offset, detail.total)
    }
    BackScaffold(title, nav) { modifier ->
        MediaGrid(paged, server.client.urls, cardShapeFor(library), "This playlist is empty", { nav.openMedia(it.id) }, modifier)
    }
}

@Composable
fun PersonScreen(id: Long, nav: Navigator) {
    val server = LocalServer.current
    val library = currentLibrary(rememberLibraries().state.valueOrNull)
    val person = rememberLoad("person:${server.id}:$id") { server.client.performer(id) }
    val paged = rememberPaged("person-media:${server.id}:$id") { limit, offset -> server.client.media(MediaQuery(performerId = id), limit, offset) }
    BackScaffold(person.state.valueOrNull?.name ?: "", nav) { modifier ->
        MediaGrid(paged, server.client.urls, cardShapeFor(library), "No titles yet", { nav.openMedia(it.id) }, modifier, header = {
            item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(maxLineSpan) }) {
                person.state.valueOrNull?.let { p ->
                    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(bottom = 8.dp)) {
                        ServerImage(p.profileImageId?.let { server.client.urls.performerImage(p.id, it) }, Modifier.size(72.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceContainerHigh)) {
                            ImagePlaceholder(Icons.Outlined.Person)
                        }
                        Spacer(Modifier.width(14.dp))
                        Column {
                            Text(p.name, style = MaterialTheme.typography.titleLarge)
                            Text("${p.mediaCount} titles", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        })
    }
}
