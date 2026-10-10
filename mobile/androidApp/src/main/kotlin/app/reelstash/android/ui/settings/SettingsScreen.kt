package app.reelstash.android.ui.settings

import android.widget.Toast
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Fingerprint
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Movie
import androidx.compose.material.icons.outlined.VideoLibrary
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.fragment.app.FragmentActivity
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.reelstash.android.BuildInfo
import app.reelstash.android.container
import app.reelstash.android.data.ThemeMode
import app.reelstash.android.deviceLock
import app.reelstash.android.lock.DeviceLock
import app.reelstash.android.ui.common.Avatar
import app.reelstash.android.ui.common.Eyebrow
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.TopBarTitle
import app.reelstash.android.ui.common.currentLibrary
import app.reelstash.android.ui.common.rememberLibraries
import app.reelstash.android.ui.common.valueOrNull
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.Rs
import app.reelstash.android.ui.theme.Tones
import app.reelstash.shared.model.SavedServers
import kotlinx.coroutines.launch

@Composable
fun SettingsScreen(nav: Navigator) {
    val context = LocalContext.current
    val container = context.container
    val lock = context.deviceLock
    val server = LocalServer.current
    val scope = rememberCoroutineScope()
    val saved by container.store.servers.collectAsStateWithLifecycle(SavedServers())
    val theme by container.theme.collectAsStateWithLifecycle()
    val lockOn by lock.enabled.collectAsStateWithLifecycle()
    val lockAfter by lock.timeoutMinutes.collectAsStateWithLifecycle()
    val libraries = rememberLibraries().state.valueOrNull
    val library = currentLibrary(libraries)
    var confirmSignOut by remember { mutableStateOf(false) }

    Column(Modifier.fillMaxSize()) {
        RsTopBar(nav) { TopBarTitle("Settings") }
        LazyColumn(Modifier.fillMaxSize().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(22.dp)) {
            item {
                Card {
                    Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        Avatar(onClick = {}, size = 48)
                        Spacer(Modifier.width(14.dp))
                        Column(Modifier.weight(1f)) {
                            Text(server.saved.user.displayName.ifBlank { "Signed in" }, style = MaterialTheme.typography.titleLarge)
                            Text("${server.saved.name} · ${server.saved.baseUrl}", style = MaterialTheme.typography.bodySmall, color = Rs.colors.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }

            item {
                Section("Library", Tones[4]) {
                    Choice("All libraries", null, Icons.Outlined.VideoLibrary, library == null) { scope.launch { container.selectLibrary(null) } }
                    libraries?.items.orEmpty().forEach { lib ->
                        Choice(lib.name, if (lib.isEntertainment) "Movies and shows" else "Videos", if (lib.isEntertainment) Icons.Outlined.Movie else Icons.Outlined.VideoLibrary, library?.id == lib.id) {
                            scope.launch { container.selectLibrary(lib) }
                        }
                    }
                }
            }

            item {
                Section("Appearance", Tones[5]) {
                    Row(Modifier.padding(12.dp).fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        ThemeMode.entries.forEach { mode ->
                            val on = mode == theme
                            Text(
                                mode.label, style = MaterialTheme.typography.labelLarge, color = if (on) Rs.colors.canvas else Rs.colors.textSecondary,
                                modifier = Modifier.weight(1f).clip(CircleShape).background(if (on) Rs.colors.text else Color.Transparent)
                                    .border(1.dp, if (on) Rs.colors.text else Rs.colors.border, CircleShape)
                                    .clickable { container.setTheme(mode) }.padding(vertical = 10.dp),
                                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                            )
                        }
                    }
                }
            }

            item {
                Section("Privacy", Tones[7]) {
                    Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        RowIcon(Icons.Outlined.Fingerprint)
                        Spacer(Modifier.width(14.dp))
                        Column(Modifier.weight(1f)) {
                            Text("App lock", style = MaterialTheme.typography.titleMedium)
                            Text("Fingerprint, face or your screen lock. Also hides ReelStash in the app switcher.", style = MaterialTheme.typography.bodySmall, color = Rs.colors.textTertiary)
                        }
                        Switch(
                            checked = lockOn,
                            onCheckedChange = { on ->
                                val activity = context as? FragmentActivity ?: return@Switch
                                if (on && !lock.available(context)) {
                                    Toast.makeText(context, "Set up a screen lock or fingerprint in Android settings first.", Toast.LENGTH_LONG).show()
                                } else lock.setEnabled(activity, on) {}
                            },
                            colors = SwitchDefaults.colors(checkedTrackColor = Rs.colors.accent, checkedThumbColor = Color.White),
                        )
                    }
                    if (lockOn) {
                        DeviceLock.TIMEOUTS.forEach { (minutes, label) ->
                            Choice(label, null, null, minutes == lockAfter) { lock.setTimeout(minutes) }
                        }
                    }
                    Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        RowIcon(Icons.Outlined.Lock)
                        Spacer(Modifier.width(14.dp))
                        Text(
                            "Your account's PIN lock is set on the web, under Settings. When it is on, the app asks for it too.",
                            style = MaterialTheme.typography.bodySmall, color = Rs.colors.textTertiary, modifier = Modifier.weight(1f),
                        )
                    }
                }
            }

            item {
                Section("Servers", Tones[3]) {
                    saved.servers.forEach { s ->
                        Choice(s.name, "${s.user.displayName} · ${s.baseUrl}", Icons.Outlined.Dns, s.id == server.id) {
                            if (s.id != server.id) scope.launch { container.switchTo(s.id) }
                        }
                    }
                    Choice("Add a server", null, Icons.Outlined.Add, false) { container.addServer() }
                    Row(Modifier.fillMaxWidth().clickable { confirmSignOut = true }.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                        RowIcon(Icons.AutoMirrored.Outlined.Logout, MaterialTheme.colorScheme.error)
                        Spacer(Modifier.width(14.dp))
                        Text("Sign out of ${server.saved.name}", style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.error)
                    }
                }
            }

            item {
                Column(Modifier.fillMaxWidth().padding(bottom = 28.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text("ReelStash for Android · ${BuildInfo.versionName(context)}", style = MaterialTheme.typography.bodySmall, color = Rs.colors.textTertiary)
                    Text("Fonts: Bricolage Grotesque and Inter, SIL Open Font License", style = MaterialTheme.typography.labelSmall, color = Rs.colors.textTertiary)
                }
            }
        }
    }

    if (confirmSignOut) {
        AlertDialog(
            onDismissRequest = { confirmSignOut = false },
            title = { Text("Sign out?") },
            text = { Text("This device is signed out of ${server.saved.name}. You can sign in again any time.") },
            confirmButton = {
                TextButton(onClick = {
                    confirmSignOut = false
                    scope.launch { container.signOut(server.id) }
                }) { Text("Sign out") }
            },
            dismissButton = { TextButton(onClick = { confirmSignOut = false }) { Text("Cancel") } },
        )
    }
}

@Composable
private fun Card(content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(18.dp)).background(Rs.colors.surface).border(1.dp, Rs.colors.border, RoundedCornerShape(18.dp)),
    ) { content() }
}

@Composable
private fun Section(title: String, tone: Color, content: @Composable () -> Unit) {
    Column {
        Eyebrow(title, Rs.colors.toneInk(tone), modifier = Modifier.padding(start = 4.dp, bottom = 10.dp))
        Card(content)
    }
}

@Composable
private fun RowIcon(icon: ImageVector, tint: Color = Rs.colors.textSecondary) {
    Icon(icon, null, tint = tint, modifier = Modifier.size(22.dp))
}

@Composable
private fun Choice(title: String, subtitle: String?, icon: ImageVector?, selected: Boolean, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 13.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (icon != null) {
            RowIcon(icon, if (selected) Rs.colors.accentText else Rs.colors.textSecondary)
            Spacer(Modifier.width(14.dp))
        }
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.titleMedium, color = Rs.colors.text)
            subtitle?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = Rs.colors.textTertiary, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
        if (selected) Icon(Icons.Filled.Check, null, tint = Rs.colors.accentText)
    }
}
