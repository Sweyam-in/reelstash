package app.reelstash.android.ui.common

import app.reelstash.shared.api.ApiException
import app.reelstash.shared.api.LockedException
import kotlinx.coroutines.CancellationException
import java.io.IOException

/** A screen's data: loading, there, or failed with a message worth showing. */
sealed interface Load<out T> {
    data object Loading : Load<Nothing>
    data class Ready<T>(val value: T) : Load<T>
    data class Failed(val message: String) : Load<Nothing>
}

val <T> Load<T>.valueOrNull: T? get() = (this as? Load.Ready)?.value

suspend fun <T> load(block: suspend () -> T): Load<T> = try {
    Load.Ready(block())
} catch (e: CancellationException) {
    throw e
} catch (e: Throwable) {
    Load.Failed(friendlyMessage(e))
}

fun friendlyMessage(e: Throwable): String = when (e) {
    is LockedException -> "ReelStash is locked"
    is ApiException -> e.message?.replaceFirstChar { it.uppercase() } ?: "The server answered ${e.status}"
    is java.net.UnknownHostException -> "Can't find that server. Check the address."
    is java.net.ConnectException -> "Can't connect to the server. Is it running, and are you on the right network?"
    is java.net.SocketTimeoutException -> "The server took too long to answer."
    is javax.net.ssl.SSLException -> "Secure connection failed. If the server uses plain HTTP, start the address with http://"
    is IOException -> e.message ?: "Network error"
    is kotlinx.serialization.SerializationException -> "That doesn't look like a ReelStash server."
    else -> e.message ?: "Something went wrong"
}
