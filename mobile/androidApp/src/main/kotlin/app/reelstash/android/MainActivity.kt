package app.reelstash.android

import android.content.Context
import android.os.Bundle
import androidx.fragment.app.FragmentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.reelstash.android.data.AppState
import app.reelstash.android.data.ThemeMode
import app.reelstash.android.lock.DeviceLockScreen
import app.reelstash.android.ui.common.LoadingBox
import app.reelstash.android.ui.connect.ConnectScreen
import app.reelstash.android.ui.lock.LockScreen
import app.reelstash.android.ui.main.MainScreen
import app.reelstash.android.ui.theme.Palettes
import app.reelstash.android.ui.theme.ReelStashTheme
import app.reelstash.shared.model.SavedServers

// A FragmentActivity because the fingerprint/face prompt needs one.
class MainActivity : FragmentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        val splash = installSplashScreen()
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        splash.setKeepOnScreenCondition { container.state.value == AppState.Starting }

        setContent {
            val theme by container.theme.collectAsStateWithLifecycle()
            val colorTheme by container.colorTheme.collectAsStateWithLifecycle()
            val lifecycleOwner = LocalLifecycleOwner.current
            DisposableEffect(lifecycleOwner) {
                val observer = LifecycleEventObserver { _, event ->
                    if (event == Lifecycle.Event.ON_RESUME) container.refreshColorTheme()
                }
                lifecycleOwner.lifecycle.addObserver(observer)
                onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
            }
            val dark = when (theme) {
                ThemeMode.Dark -> true
                ThemeMode.Light -> false
                ThemeMode.System -> androidx.compose.foundation.isSystemInDarkTheme()
            }
            ReelStashTheme(dark = dark, palette = Palettes.byId(colorTheme)) {
                val state by container.state.collectAsStateWithLifecycle()
                val deviceLocked by deviceLock.locked.collectAsStateWithLifecycle()
                if (deviceLocked) {
                    DeviceLockScreen(this, deviceLock)
                    return@ReelStashTheme
                }
                when (val s = state) {
                    AppState.Starting -> LoadingBox()
                    AppState.NeedsServer -> {
                        val saved by container.store.servers.collectAsStateWithLifecycle(SavedServers())
                        ConnectScreen(canCancel = saved.servers.isNotEmpty(), onCancel = container::cancelAddServer)
                    }
                    is AppState.Ready -> if (s.locked) LockScreen(s.server) else {
                        // Switching server or library starts the screens afresh.
                        key(s.server.id, s.server.saved.libraryId) { MainScreen(s.server) }
                    }
                }
            }
        }
    }
}

object BuildInfo {
    fun versionName(context: Context): String =
        runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }.getOrNull() ?: "?"
}
