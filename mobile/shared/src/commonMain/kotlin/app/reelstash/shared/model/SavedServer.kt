package app.reelstash.shared.model

import kotlinx.serialization.Serializable

/** A server the app has signed in to, as kept on the device. */
@Serializable
data class SavedServer(
    /** Stable local id (the normalized base URL). */
    val id: String,
    val baseUrl: String,
    val name: String,
    val token: String,
    val user: User,
    val libraryId: Long? = null,
)

@Serializable
data class SavedServers(val servers: List<SavedServer> = emptyList(), val currentId: String? = null) {
    val current: SavedServer? get() = servers.firstOrNull { it.id == currentId } ?: servers.firstOrNull()

    fun upsert(server: SavedServer) = copy(servers = servers.filterNot { it.id == server.id } + server, currentId = server.id)

    fun remove(id: String): SavedServers {
        val rest = servers.filterNot { it.id == id }
        return copy(servers = rest, currentId = if (currentId == id) rest.firstOrNull()?.id else currentId)
    }
}
