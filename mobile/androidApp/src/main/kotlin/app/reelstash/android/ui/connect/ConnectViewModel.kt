package app.reelstash.android.ui.connect

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.reelstash.android.data.AppContainer
import app.reelstash.android.ui.common.friendlyMessage
import app.reelstash.shared.ServerAddress
import app.reelstash.shared.api.ApiException
import app.reelstash.shared.api.ReelStashClient
import app.reelstash.shared.model.PairingStart
import app.reelstash.shared.model.ServerInfo
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class ConnectUiState(
    val address: String = "",
    val checking: Boolean = false,
    val error: String? = null,
    /** Set once the address answered as a ReelStash server. */
    val server: FoundServer? = null,
    val signingIn: Boolean = false,
    val pairing: PairingStart? = null,
    val pairingError: String? = null,
)

data class FoundServer(val baseUrl: String, val info: ServerInfo)

/** Minimum server API this app speaks; older servers lack app sign-in. */
private const val MIN_API_VERSION = 1

class ConnectViewModel(private val container: AppContainer) : ViewModel() {
    private val _state = MutableStateFlow(ConnectUiState())
    val state: StateFlow<ConnectUiState> = _state.asStateFlow()

    private var probe: ReelStashClient? = null
    private var pairingJob: Job? = null

    fun setAddress(value: String) = _state.update { it.copy(address = value, error = null) }

    fun check() {
        val url = when (val r = ServerAddress.normalize(_state.value.address)) {
            is ServerAddress.Result.Invalid -> return _state.update { it.copy(error = r.reason) }
            is ServerAddress.Result.Ok -> r.url
        }
        _state.update { it.copy(checking = true, error = null) }
        viewModelScope.launch {
            val client = container.newClient(url)
            val result = runCatching { client.serverInfo() }
            val info = result.getOrNull()
            val error = when {
                info == null -> (result.exceptionOrNull() as? ApiException)?.takeIf { it.status == 404 }
                    ?.let { "This server needs an update before the app can sign in (it has no app sign-in yet)." }
                    ?: friendlyMessage(result.exceptionOrNull()!!)
                info.product != "reelstash" -> "That doesn't look like a ReelStash server."
                info.apiVersion < MIN_API_VERSION -> "This server is too old for the app. Update ReelStash on the server."
                else -> null
            }
            if (error != null) {
                client.close()
                _state.update { it.copy(checking = false, error = error) }
            } else {
                probe?.close()
                probe = client
                runCatching { client.authConfig() }.onSuccess { container.setColorTheme(it.colorTheme) }
                _state.update { it.copy(checking = false, server = FoundServer(url, info!!)) }
                if (!info!!.passwordLogin) startPairing()
            }
        }
    }

    fun back() {
        pairingJob?.cancel()
        _state.update { ConnectUiState(address = it.address) }
    }

    fun signIn(username: String, password: String) {
        val server = _state.value.server ?: return
        val client = probe ?: return
        _state.update { it.copy(signingIn = true, error = null) }
        viewModelScope.launch {
            runCatching { client.passwordSignIn(username.trim(), password, container.device) }
                .onSuccess { session ->
                    pairingJob?.cancel()
                    container.signedIn(server.baseUrl, server.info.name, session)
                }
                .onFailure { e -> _state.update { it.copy(signingIn = false, error = friendlyMessage(e)) } }
        }
    }

    /** Shows a code to enter on the web, and waits for someone to approve it. */
    fun startPairing() {
        val server = _state.value.server ?: return
        val client = probe ?: return
        pairingJob?.cancel()
        _state.update { it.copy(pairing = null, pairingError = null) }
        pairingJob = viewModelScope.launch {
            val start = runCatching { client.startPairing(container.device) }.getOrElse { e ->
                _state.update { it.copy(pairingError = friendlyMessage(e)) }
                return@launch
            }
            _state.update { it.copy(pairing = start) }
            val deadline = System.currentTimeMillis() + start.expiresIn * 1000L
            while (System.currentTimeMillis() < deadline) {
                delay(start.interval.coerceAtLeast(2) * 1000L)
                val session = try {
                    client.pollPairing(start.deviceCode)
                } catch (e: ApiException) {
                    if (e.status == 404) break else continue
                } catch (e: Exception) {
                    continue // a network blip; keep waiting
                }
                if (session != null) {
                    container.signedIn(server.baseUrl, server.info.name, session)
                    return@launch
                }
            }
            _state.update { it.copy(pairing = null, pairingError = "The code expired.") }
        }
    }

    fun stopPairing() {
        pairingJob?.cancel()
        _state.update { it.copy(pairing = null, pairingError = null) }
    }

    override fun onCleared() {
        probe?.close()
    }
}
