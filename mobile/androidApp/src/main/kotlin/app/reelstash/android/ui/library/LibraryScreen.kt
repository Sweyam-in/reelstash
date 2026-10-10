package app.reelstash.android.ui.library

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.PlaylistPlay
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.BookmarkBorder
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.common.ImagePlaceholder
import app.reelstash.android.ui.common.Load
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.SectionHeader
import app.reelstash.android.ui.common.TopBarTitle
import app.reelstash.android.ui.common.ServerImage
import app.reelstash.android.ui.common.rememberLoad
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.shared.api.MediaUrls

/** The signed-in person's own lists: favorites, watch later and playlists. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LibraryScreen(nav: Navigator) {
    val server = LocalServer.current
    val playlists = rememberLoad("playlists:${server.id}:${server.saved.libraryId}") { server.client.playlists().items }

    androidx.compose.foundation.layout.Column(Modifier.fillMaxSize()) {
        RsTopBar(nav) { TopBarTitle("My stuff") }
        LazyColumn(Modifier.fillMaxSize()) {
            item {
                ListItem(
                    headlineContent = { Text("Favorites") },
                    leadingContent = { Icon(Icons.Outlined.FavoriteBorder, null) },
                    modifier = Modifier.clickable { nav.openList("favorites") },
                )
            }
            item {
                ListItem(
                    headlineContent = { Text("Watch later") },
                    leadingContent = { Icon(Icons.Outlined.BookmarkBorder, null) },
                    modifier = Modifier.clickable { nav.openList("watchlater") },
                )
            }
            item { SectionHeader("Playlists", "Yours", modifier = Modifier.padding(top = 16.dp, bottom = 8.dp)) }
            when (val state = playlists.state) {
                Load.Loading -> {}
                is Load.Failed -> item {
                    ListItem(headlineContent = { Text("Couldn't load playlists") }, supportingContent = { Text(state.message) }, modifier = Modifier.clickable { playlists.retry() })
                }
                is Load.Ready -> {
                    if (state.value.isEmpty()) item {
                        ListItem(headlineContent = { Text("No playlists yet") }, supportingContent = { Text("Make them in ReelStash on the web; they show up here.") })
                    }
                    items(state.value, key = { it.id }) { playlist ->
                        ListItem(
                            headlineContent = { Text(playlist.name) },
                            supportingContent = { Text("${playlist.mediaCount} titles" + if (playlist.smart) " · Smart" else "") },
                            leadingContent = {
                                Box(Modifier.size(64.dp, 40.dp).clip(RoundedCornerShape(6.dp))) {
                                    val cover = playlist.covers.firstOrNull()
                                    if (cover != null) ServerImage(server.client.urls.artwork(cover.id, MediaUrls.Artwork.Thumbnail), Modifier.fillMaxSize()) { app.reelstash.android.ui.common.ToneArt("c${playlist.id}", playlist.name, null, Modifier.fillMaxSize(), glyphSize = 22.dp) }
                                    else app.reelstash.android.ui.common.ToneArt("c${playlist.id}", playlist.name, null, Modifier.fillMaxSize(), glyphSize = 22.dp)
                                }
                            },
                            modifier = Modifier.clickable { nav.openPlaylist(playlist.id) },
                        )
                    }
                }
            }
        }
    }
}
