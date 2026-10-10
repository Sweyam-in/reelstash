package app.reelstash.android.ui.browse

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Sort
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.common.CardShape
import app.reelstash.android.ui.common.LibraryPicker
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.TopBarTitle
import app.reelstash.android.ui.common.ambientGlow
import app.reelstash.android.ui.common.cardShapeFor
import app.reelstash.android.ui.common.currentLibrary
import app.reelstash.android.ui.common.rememberLibraries
import app.reelstash.android.ui.common.rememberPaged
import app.reelstash.android.ui.common.valueOrNull
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.Rs
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.model.Library

private enum class Section(val label: String) { Movies("Movies"), Shows("Shows"), Videos("Videos"), Series("Series"), People("People") }

private fun sectionsFor(library: Library?) = when {
    library?.isEntertainment == true -> listOf(Section.Movies, Section.Shows, Section.People)
    library != null -> listOf(Section.Videos, Section.Series, Section.People)
    else -> listOf(Section.Videos, Section.Shows, Section.People)
}

@Composable
fun BrowseScreen(nav: Navigator) {
    val server = LocalServer.current
    val libraries = rememberLibraries()
    val library = currentLibrary(libraries.state.valueOrNull)
    val sections = sectionsFor(library)
    var selected by rememberSaveable { mutableStateOf(0) }
    val section = sections[selected.coerceIn(sections.indices)]
    var sort by rememberSaveable { mutableStateOf(MediaQuery.Sort.Newest) }
    val scope = "${server.id}:${server.saved.libraryId}"

    Column(Modifier.fillMaxSize().ambientGlow(Rs.colors.glow, .35f)) {
        RsTopBar(nav) { LibraryPicker(libraries.state.valueOrNull) }
        FilterRow(sections, selected, onSelect = { selected = it }) {
            if (section == Section.Movies || section == Section.Videos) SortMenu(sort) { sort = it }
        }
        Box(Modifier.fillMaxSize()) {
            when (section) {
                Section.Movies, Section.Videos -> {
                    val query = MediaQuery(kind = if (section == Section.Movies) MediaQuery.Kind.Movie else null, sort = sort)
                    val paged = rememberPaged("media:$scope:$query") { limit, offset -> server.client.media(query, limit, offset) }
                    MediaGrid(paged, server.client.urls, if (section == Section.Movies) CardShape.Poster else cardShapeFor(library), "No titles here yet", { nav.openMedia(it.id) })
                }
                Section.Shows, Section.Series -> {
                    val shows = section == Section.Shows
                    val paged = rememberPaged("series:$scope:$shows") { limit, offset ->
                        if (shows) server.client.shows(limit, offset) else server.client.series(limit, offset)
                    }
                    PagedGrid(paged, 120.dp, if (shows) "No shows yet" else "No series yet", key = { it.id }) { s ->
                        SeriesCard(s, server.client.urls) { nav.openSeries(s.id) }
                    }
                }
                Section.People -> PeopleGrid(nav)
            }
        }
    }
}

/** Rounded filter chips under the bar, with room for a trailing action. */
@Composable
private fun FilterRow(sections: List<Section>, selected: Int, onSelect: (Int) -> Unit, trailing: @Composable () -> Unit) {
    val c = Rs.colors
    Row(Modifier.padding(bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        LazyRow(Modifier.weight(1f), contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(sections.size) { i ->
                val on = i == selected
                Text(
                    sections[i].label,
                    style = MaterialTheme.typography.labelLarge,
                    color = if (on) c.canvas else c.textSecondary,
                    modifier = Modifier.clip(CircleShape)
                        .background(if (on) c.text else c.surface)
                        .border(1.dp, if (on) c.text else c.border, CircleShape)
                        .clickable { onSelect(i) }
                        .padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }
        }
        trailing()
    }
}

@Composable
private fun SortMenu(sort: MediaQuery.Sort, onSort: (MediaQuery.Sort) -> Unit) {
    var open by remember { mutableStateOf(false) }
    Box(Modifier.padding(end = 10.dp)) {
        Box(
            Modifier.height(36.dp).clip(CircleShape).background(Rs.colors.surface).border(1.dp, Rs.colors.border, CircleShape)
                .clickable { open = true }.padding(horizontal = 10.dp),
            contentAlignment = Alignment.Center,
        ) { Icon(Icons.AutoMirrored.Filled.Sort, "Sort", tint = Rs.colors.text) }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            listOf(MediaQuery.Sort.Newest, MediaQuery.Sort.Oldest, MediaQuery.Sort.Title, MediaQuery.Sort.Shuffle).forEach { s ->
                DropdownMenuItem(
                    text = { Text(s.label) },
                    onClick = { open = false; onSort(s) },
                    trailingIcon = { if (s == sort) Icon(Icons.Filled.Check, null, tint = Rs.colors.accentText) },
                )
            }
        }
    }
}

@Composable
private fun PeopleGrid(nav: Navigator) {
    val server = LocalServer.current
    val paged = rememberPaged("people:${server.id}:${server.saved.libraryId}") { limit, offset -> server.client.performers(limit, offset) }
    PagedGrid(paged, 100.dp, "No people yet", key = { it.id }) { p -> PerformerCard(p, server.client.urls) { nav.openPerson(p.id) } }
}

/** Everyone in the library, from Home's "All" link. */
@Composable
fun PeopleScreen(nav: Navigator) {
    Column(Modifier.fillMaxSize()) {
        RsTopBar(nav, onBack = { nav.back() }) { TopBarTitle("People") }
        PeopleGrid(nav)
    }
}

/** Favorites, Watch later and other fixed lists, reached from "My stuff". */
@Composable
fun MediaListScreen(kind: String, nav: Navigator) {
    val server = LocalServer.current
    val library = currentLibrary(rememberLibraries().state.valueOrNull)
    val (title, query) = when (kind) {
        "favorites" -> "Favorites" to MediaQuery(favorite = true)
        "watchlater" -> "Watch later" to MediaQuery(watchLater = true)
        else -> "Titles" to MediaQuery()
    }
    val paged = rememberPaged("list:${server.id}:${server.saved.libraryId}:$kind") { limit, offset -> server.client.media(query, limit, offset) }
    Column(Modifier.fillMaxSize()) {
        RsTopBar(nav, onBack = { nav.back() }) { TopBarTitle(title) }
        MediaGrid(paged, server.client.urls, cardShapeFor(library), "Nothing here yet", { nav.openMedia(it.id) })
    }
}
