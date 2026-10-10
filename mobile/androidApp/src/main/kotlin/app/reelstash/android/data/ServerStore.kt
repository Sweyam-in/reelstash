package app.reelstash.android.data

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import app.reelstash.shared.api.ReelStashJson
import app.reelstash.shared.model.SavedServer
import app.reelstash.shared.model.SavedServers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.map

private val Context.dataStore by preferencesDataStore(name = "servers")
private val SERVERS = stringPreferencesKey("servers")

/**
 * The servers this device is signed in to, with their tokens. Kept in the
 * app's private storage, which backups exclude (see data_extraction_rules).
 */
class ServerStore(private val context: Context) {
    val servers: Flow<SavedServers> = context.dataStore.data.map { prefs -> decode(prefs[SERVERS]) }

    suspend fun current(): SavedServers = servers.first()

    suspend fun update(change: (SavedServers) -> SavedServers) {
        context.dataStore.edit { prefs ->
            prefs[SERVERS] = ReelStashJson.encodeToString(change(decode(prefs[SERVERS])))
        }
    }

    private fun decode(raw: String?): SavedServers =
        raw?.let { runCatching { ReelStashJson.decodeFromString<SavedServers>(it) }.getOrNull() } ?: SavedServers()
}

fun SavedServers.withLibrary(serverId: String, libraryId: Long?) =
    copy(servers = servers.map { if (it.id == serverId) it.copy(libraryId = libraryId) else it })

fun SavedServer.withToken(token: String) = copy(token = token)
