package app.reelstash.android.ui.search

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.browse.MediaGrid
import app.reelstash.android.ui.common.MessageBox
import app.reelstash.android.ui.common.cardShapeFor
import app.reelstash.android.ui.common.currentLibrary
import app.reelstash.android.ui.common.rememberLibraries
import app.reelstash.android.ui.common.rememberPaged
import app.reelstash.android.ui.common.valueOrNull
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.model.SearchHit
import kotlinx.coroutines.delay

@Composable
fun SearchScreen(nav: Navigator) {
    val server = LocalServer.current
    val library = currentLibrary(rememberLibraries().state.valueOrNull)
    var text by rememberSaveable { mutableStateOf("") }
    // Search as you type, once typing pauses.
    val query by produceState(text, text) {
        delay(350)
        value = text.trim()
    }
    val focus = androidx.compose.runtime.remember { FocusRequester() }
    val keyboard = LocalSoftwareKeyboardController.current
    LaunchedEffect(Unit) { if (text.isEmpty()) focus.requestFocus() }

    Column(Modifier.fillMaxSize()) {
        app.reelstash.android.ui.common.RsTopBar(nav, onBack = { nav.back() }, showSearch = false) { app.reelstash.android.ui.common.TopBarTitle("Search") }
        Column(Modifier.fillMaxSize()) {
            OutlinedTextField(
                value = text,
                onValueChange = { text = it },
                placeholder = { Text("Titles, people, studios") },
                leadingIcon = { Icon(Icons.Outlined.Search, null) },
                trailingIcon = { if (text.isNotEmpty()) IconButton(onClick = { text = "" }) { Icon(Icons.Filled.Close, "Clear") } },
                singleLine = true,
                shape = MaterialTheme.shapes.extraLarge,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                keyboardActions = KeyboardActions(onSearch = { keyboard?.hide() }),
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp).focusRequester(focus),
            )
            if (query.length < 2) {
                MessageBox("Search your library", "Type at least two letters.")
                return@Column
            }
            val people by produceState(emptyList<SearchHit>(), query, server.id) {
                value = runCatching { server.client.search(query).items.filter { it.type == SearchHit.PERFORMER } }.getOrDefault(emptyList())
            }
            val mq = MediaQuery(text = query)
            val paged = rememberPaged("search:${server.id}:${server.saved.libraryId}:$query") { limit, offset -> server.client.media(mq, limit, offset) }
            MediaGrid(paged, server.client.urls, cardShapeFor(library), "No titles match “$query”", { nav.openMedia(it.id) }, header = {
                if (people.isNotEmpty()) {
                    item(span = { GridItemSpan(maxLineSpan) }) {
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp), contentPadding = PaddingValues(bottom = 4.dp)) {
                            items(people, key = { it.id }) { hit ->
                                AssistChip(onClick = { nav.openPerson(hit.id) }, label = { Text(hit.label) }, leadingIcon = { Icon(Icons.Outlined.Person, null) })
                            }
                        }
                    }
                }
            })
        }
    }
}
