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
 * app's private storage (excluded from backup) and sealed with [TokenVault].
 */
class ServerStore(
    private val context: Context,
    private val vault: TokenVault = TokenVault(),
) {
    val servers: Flow<SavedServers> = context.dataStore.data.map { prefs -> decode(prefs[SERVERS]) }

    suspend fun current(): SavedServers = servers.first()

    suspend fun update(change: (SavedServers) -> SavedServers) {
        context.dataStore.edit { prefs ->
            prefs[SERVERS] = vault.seal(ReelStashJson.encodeToString(change(decode(prefs[SERVERS]))))
        }
    }

    /** Rewrites a plaintext install as a sealed blob. Safe to call on every launch. */
    suspend fun migrateToEncrypted() {
        context.dataStore.edit { prefs ->
            val raw = prefs[SERVERS] ?: return@edit
            if (vault.isSealed(raw)) return@edit
            // Round-trip through decode so corrupt plaintext is dropped, not sealed.
            val json = ReelStashJson.encodeToString(decode(raw))
            prefs[SERVERS] = vault.seal(json)
        }
    }

    private fun decode(raw: String?): SavedServers {
        if (raw.isNullOrBlank()) return SavedServers()
        val plain = runCatching { vault.open(raw) }.getOrNull() ?: return SavedServers()
        return runCatching { ReelStashJson.decodeFromString<SavedServers>(plain) }.getOrNull() ?: SavedServers()
    }
}

fun SavedServers.withLibrary(serverId: String, libraryId: Long?) =
    copy(servers = servers.map { if (it.id == serverId) it.copy(libraryId = libraryId) else it })

fun SavedServer.withToken(token: String) = copy(token = token)
