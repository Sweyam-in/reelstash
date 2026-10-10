package app.reelstash.android.ui.common

import androidx.compose.foundation.layout.Row
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowDropDown
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalContext
import app.reelstash.android.container
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.shared.model.Libraries
import app.reelstash.shared.model.Library
import kotlinx.coroutines.launch

/** The libraries this account can open; loaded once per screen. */
@Composable
fun rememberLibraries(): Loaded<Libraries> {
    val server = LocalServer.current
    return rememberLoad("libraries:${server.id}") { server.client.libraries() }
}

/** The library being browsed, or null for "all libraries". */
@Composable
fun currentLibrary(libraries: Libraries?): Library? {
    val id = LocalServer.current.saved.libraryId ?: return libraries?.items?.singleOrNull()
    return libraries?.items?.firstOrNull { it.id == id }
}

/** Title-bar switcher between the libraries the account can open. */
@Composable
fun LibraryPicker(libraries: Libraries?) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val current = currentLibrary(libraries)
    var open by remember { mutableStateOf(false) }
    val items = libraries?.items.orEmpty()
    TextButton(onClick = { if (items.size > 1) open = true }) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(current?.name ?: if (items.size > 1) "All libraries" else "ReelStash", style = MaterialTheme.typography.titleLarge, color = MaterialTheme.colorScheme.onSurface)
            if (items.size > 1) Icon(Icons.Filled.ArrowDropDown, "Switch library", tint = MaterialTheme.colorScheme.onSurface)
        }
    }
    DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
        val choose = { library: Library? ->
            open = false
            scope.launch { context.container.selectLibrary(library) }
            Unit
        }
        DropdownMenuItem(
            text = { Text("All libraries") },
            onClick = { choose(null) },
            trailingIcon = { if (current == null) Icon(Icons.Filled.Check, null) },
        )
        items.forEach { library ->
            DropdownMenuItem(
                text = { Text(library.name) },
                onClick = { choose(library) },
                leadingIcon = if (library.locked) ({ Icon(Icons.Outlined.Lock, "Locked") }) else null,
                trailingIcon = { if (current?.id == library.id) Icon(Icons.Filled.Check, null) },
            )
        }
    }
}

/** Entertainment libraries show posters; private ones (and "all") show landscape thumbnails. */
fun cardShapeFor(library: Library?) = if (library?.isEntertainment == true) CardShape.Poster else CardShape.Landscape
