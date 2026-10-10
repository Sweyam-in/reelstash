package app.reelstash.android.data

import android.content.Context
import android.os.Build
import android.provider.Settings
import app.reelstash.android.device.AndroidCapabilities
import app.reelstash.shared.api.DeviceDescription
import app.reelstash.shared.api.ReelStashClient
import app.reelstash.shared.api.ServerCredentials
import app.reelstash.shared.api.SessionEvent
import app.reelstash.shared.model.DeviceSession
import app.reelstash.shared.model.Library
import app.reelstash.shared.model.SavedServer
import app.reelstash.shared.model.normalizeColorTheme
import io.ktor.client.engine.okhttp.OkHttp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Interceptor
import okhttp3.OkHttpClient
import java.util.concurrent.TimeUnit

enum class ThemeMode(val label: String) { Dark("Dark"), Light("Light"), System("System") }

/** The server the app is using right now, ready to call. */
class ActiveServer(val saved: SavedServer, val client: ReelStashClient) {
    val credentials: ServerCredentials get() = client.credentials
    val id: String get() = saved.id
}

/** What the app shows at the top level. */
sealed interface AppState {
    data object Starting : AppState
    /** No server yet, or the user chose to add one. */
    data object NeedsServer : AppState
    data class Ready(val server: ActiveServer, val locked: Boolean = false) : AppState
}

/**
 * Hand-built dependency graph: one per process, owned by [app.reelstash.android.ReelStashApplication].
 */
class AppContainer(context: Context) {
    private val appContext = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    val store = ServerStore(appContext)
    val capabilities by lazy { AndroidCapabilities.detect() }

    val device = DeviceDescription(
        name = runCatching { Settings.Global.getString(appContext.contentResolver, Settings.Global.DEVICE_NAME) }.getOrNull()
            ?.takeIf { it.isNotBlank() } ?: "${Build.MANUFACTURER.replaceFirstChar { it.uppercase() }} ${Build.MODEL}",
        platform = "android",
    )

    private val uiPrefs = appContext.getSharedPreferences("ui", Context.MODE_PRIVATE)
    private val _theme = MutableStateFlow(runCatching { ThemeMode.valueOf(uiPrefs.getString("theme", null) ?: "") }.getOrDefault(ThemeMode.Dark))
    /** Dark (Neelam, the web's default), Light (Mukta) or following the system. */
    val theme: StateFlow<ThemeMode> = _theme.asStateFlow()

    fun setTheme(mode: ThemeMode) {
        uiPrefs.edit().putString("theme", mode.name).apply()
        _theme.value = mode
    }

    private val _state = MutableStateFlow<AppState>(AppState.Starting)
    val state: StateFlow<AppState> = _state.asStateFlow()

    private val _colorTheme = MutableStateFlow("grove")
    /** The admin-chosen library palette. Grove until a server answers. */
    val colorTheme: StateFlow<String> = _colorTheme.asStateFlow()

    fun setColorTheme(id: String?) {
        _colorTheme.value = normalizeColorTheme(id)
    }

    fun refreshColorTheme() {
        val client = active?.client ?: return
        scope.launch {
            runCatching { client.authConfig().colorTheme }.onSuccess(::setColorTheme)
        }
    }

    private val active get() = (_state.value as? AppState.Ready)?.server

    /** Plain client for Ktor; [ReelStashClient] adds its own headers. */
    private val baseHttp: OkHttpClient = OkHttpClient.Builder()
        .connectTimeout(10, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .build()

    /**
     * For the player and image loading: adds the active server's credentials,
     * but only to requests for that server, so a token never leaks elsewhere.
     */
    val mediaHttp: OkHttpClient = baseHttp.newBuilder()
        .readTimeout(60, TimeUnit.SECONDS)
        .addInterceptor(Interceptor { chain ->
            val request = chain.request()
            val server = active
            val base = server?.client?.baseUrl?.toHttpUrlOrNull()
            val sameServer = base != null && request.url.host == base.host && request.url.port == base.port
            val response = if (sameServer) {
                val builder = request.newBuilder()
                server!!.credentials.headers().forEach { (k, v) -> builder.header(k, v) }
                chain.proceed(builder.build())
            } else chain.proceed(request)
            if (sameServer) response.header(ServerCredentials.HEADER_UNLOCK)?.let { server!!.credentials.unlockToken = it }
            if (sameServer && response.code == 423) markLocked()
            response
        })
        .build()

    fun newClient(baseUrl: String, credentials: ServerCredentials = ServerCredentials()) =
        ReelStashClient(baseUrl, credentials, OkHttp.create { preconfigured = baseHttp })

    private var eventsJob: Job? = null

    fun start() {
        scope.launch {
            val saved = store.current().current
            if (saved == null) _state.value = AppState.NeedsServer else activate(saved)
        }
    }

    private fun activate(saved: SavedServer) {
        active?.client?.close()
        val client = newClient(saved.baseUrl, ServerCredentials(token = saved.token, libraryId = saved.libraryId))
        val server = ActiveServer(saved, client)
        _state.value = AppState.Ready(server)
        refreshColorTheme()
        eventsJob?.cancel()
        eventsJob = scope.launch {
            client.events.collect { event ->
                when (event) {
                    SessionEvent.SignedOut -> signOut(server.id, revoke = false)
                    SessionEvent.Locked -> markLocked()
                }
            }
        }
    }

    /** Keeps a fresh device session and switches to that server. */
    suspend fun signedIn(baseUrl: String, serverName: String, session: DeviceSession) {
        val saved = SavedServer(id = baseUrl, baseUrl = baseUrl, name = serverName, token = session.token, user = session.user)
        store.update { it.upsert(saved) }
        activate(saved)
    }

    suspend fun switchTo(serverId: String) {
        val all = store.current()
        val saved = all.servers.firstOrNull { it.id == serverId } ?: return
        store.update { it.copy(currentId = serverId) }
        activate(saved)
    }

    fun addServer() {
        _state.value = AppState.NeedsServer
    }

    /** Back from "add a server" without adding one. */
    fun cancelAddServer() {
        scope.launch { store.current().current?.let { activate(it) } }
    }

    suspend fun selectLibrary(library: Library?) {
        val server = active ?: return
        server.credentials.libraryId = library?.id
        store.update { it.withLibrary(server.id, library?.id) }
        _state.value = AppState.Ready(ActiveServer(server.saved.copy(libraryId = library?.id), server.client))
    }

    /** Signs out of a server; revoke also ends the session on the server. */
    suspend fun signOut(serverId: String, revoke: Boolean = true) {
        val server = active
        if (revoke && server?.id == serverId) server.client.signOut()
        store.update { it.remove(serverId) }
        val next = store.current().current
        if (next == null) {
            server?.client?.close()
            _state.value = AppState.NeedsServer
        } else if (server?.id == serverId) activate(next)
    }

    fun markLocked() {
        val ready = _state.value as? AppState.Ready ?: return
        if (!ready.locked) _state.value = ready.copy(locked = true)
    }

    fun markUnlocked() {
        val ready = _state.value as? AppState.Ready ?: return
        _state.value = ready.copy(locked = false)
    }
}
