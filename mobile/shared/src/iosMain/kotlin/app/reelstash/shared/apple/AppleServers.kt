package app.reelstash.shared.apple

import app.reelstash.shared.ServerAddress
import app.reelstash.shared.api.ReelStashJson
import app.reelstash.shared.model.DeviceSession
import app.reelstash.shared.model.SavedServer
import app.reelstash.shared.model.SavedServers
import kotlinx.serialization.Serializable

/** A normalized address, or the reason it was rejected. Exactly one field is set. */
@Serializable
data class AppleAddress(val url: String? = null, val error: String? = null)

fun normalizeServerAddress(input: String): AppleAddress = when (val result = ServerAddress.normalize(input)) {
    is ServerAddress.Result.Ok -> AppleAddress(url = result.url)
    is ServerAddress.Result.Invalid -> AppleAddress(error = result.reason)
}

/**
 * The signed-in servers, as one JSON document the Swift app stores.
 * Mutations stay here so Swift never has to call a data-class `copy`.
 */
object AppleServers {
    fun empty(): String = ReelStashJson.encodeToString(SavedServers())

    fun decode(raw: String?): SavedServers =
        raw?.takeIf { it.isNotBlank() }?.let { runCatching { ReelStashJson.decodeFromString<SavedServers>(it) }.getOrNull() }
            ?: SavedServers()

    fun current(raw: String?): SavedServer? = decode(raw).current

    fun list(raw: String?): List<SavedServer> = decode(raw).servers

    fun rememberSignIn(raw: String?, baseUrl: String, serverName: String, session: DeviceSession): String {
        val saved = SavedServer(
            id = baseUrl,
            baseUrl = baseUrl,
            name = serverName.ifBlank { "ReelStash" },
            token = session.token,
            user = session.user,
        )
        return ReelStashJson.encodeToString(decode(raw).upsert(saved))
    }

    fun select(raw: String?, id: String): String {
        val decoded = decode(raw)
        if (decoded.servers.none { it.id == id }) return ReelStashJson.encodeToString(decoded)
        return ReelStashJson.encodeToString(decoded.copy(currentId = id))
    }

    fun remove(raw: String?, id: String): String = ReelStashJson.encodeToString(decode(raw).remove(id))

    /** [libraryId] below zero clears the selection (all libraries the account can see). */
    fun withLibrary(raw: String?, serverId: String, libraryId: Long): String {
        val id = libraryId.takeIf { it >= 0 }
        val updated = decode(raw).let { book ->
            book.copy(servers = book.servers.map { if (it.id == serverId) it.copy(libraryId = id) else it })
        }
        return ReelStashJson.encodeToString(updated)
    }
}
