package app.reelstash.android.player

import android.app.PictureInPictureParams
import android.content.Context
import android.content.Intent
import android.content.pm.ActivityInfo
import android.os.Build
import android.os.Bundle
import android.util.Rational
import app.reelstash.android.R
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.annotation.OptIn
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.initializer
import androidx.lifecycle.viewmodel.viewModelFactory
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.PlayerView
import app.reelstash.android.container
import app.reelstash.android.deviceLock
import app.reelstash.android.data.AppState
import app.reelstash.android.ui.theme.Palettes
import app.reelstash.android.ui.theme.ReelStashTheme

/** Full-screen playback, in its own activity so it can go landscape and picture-in-picture. */
class PlayerActivity : ComponentActivity() {
    private val vm: PlayerViewModel by viewModels {
        viewModelFactory { initializer { PlayerViewModel(application, container, intent.getLongExtra(EXTRA_ID, 0)) } }
    }

    @Suppress("DEPRECATION")
    override fun finish() {
        super.finish()
        if (Build.VERSION.SDK_INT < 34) overridePendingTransition(R.anim.stay, R.anim.player_exit)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        if (Build.VERSION.SDK_INT >= 34) {
            overrideActivityTransition(OVERRIDE_TRANSITION_OPEN, R.anim.player_enter, R.anim.stay)
            overrideActivityTransition(OVERRIDE_TRANSITION_CLOSE, R.anim.stay, R.anim.player_exit)
        }
        // The player needs a signed-in server; after process death without one, there is nothing to play.
        if (container.state.value !is AppState.Ready) {
            finish()
            return
        }
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        WindowCompat.setDecorFitsSystemWindows(window, false)
        WindowInsetsControllerCompat(window, window.decorView).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
        setContent {
            val colorTheme by container.colorTheme.collectAsStateWithLifecycle()
            ReelStashTheme(dark = true, palette = Palettes.byId(colorTheme)) {
                val state by vm.state.collectAsStateWithLifecycle()
                // Back after the app lock engaged: close, so the lock screen underneath shows.
                val deviceLocked by deviceLock.locked.collectAsStateWithLifecycle()
                LaunchedEffect(deviceLocked) { if (deviceLocked) finish() }
                // Landscape videos turn the screen sideways; vertical ones (reels) stay upright.
                LaunchedEffect(state.portrait) {
                    requestedOrientation = when (state.portrait) {
                        true -> ActivityInfo.SCREEN_ORIENTATION_SENSOR_PORTRAIT
                        false -> ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE
                        null -> ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                    }
                }
                PlayerScreen(vm, state, onBack = ::finish)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        vm.open(intent.getLongExtra(EXTRA_ID, 0))
    }

    override fun onUserLeaveHint() {
        super.onUserLeaveHint()
        if (vm.player.isPlaying && Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val portrait = vm.state.value.portrait == true
            runCatching {
                enterPictureInPictureMode(PictureInPictureParams.Builder().setAspectRatio(if (portrait) Rational(9, 16) else Rational(16, 9)).build())
            }
        }
    }

    // Save on pause, not stop: the screen underneath resumes (and re-reads the
    // resume point) before this activity is stopped.
    override fun onPause() {
        super.onPause()
        vm.save()
    }

    override fun onStop() {
        super.onStop()
        // Leaving, not just shrinking into picture-in-picture.
        if (!isInPictureInPictureMode) vm.player.pause()
    }

    companion object {
        private const val EXTRA_ID = "media_id"
        fun intent(context: Context, mediaId: Long) = Intent(context, PlayerActivity::class.java).putExtra(EXTRA_ID, mediaId)
    }
}

@OptIn(UnstableApi::class)
@Composable
private fun PlayerScreen(vm: PlayerViewModel, state: PlayerUiState, onBack: () -> Unit) {
    // The controller shows itself when playback starts; the listener keeps the title bar in step.
    var controlsVisible by remember { mutableStateOf(false) }
    Box(Modifier.fillMaxSize().background(Color.Black)) {
        AndroidView(
            factory = { context ->
                PlayerView(context).apply {
                    player = vm.player
                    setShowSubtitleButton(true)
                    setShowNextButton(false)
                    setShowPreviousButton(false)
                    setControllerVisibilityListener(PlayerView.ControllerVisibilityListener { v -> controlsVisible = v == android.view.View.VISIBLE })
                    keepScreenOn = true
                }
            },
            modifier = Modifier.fillMaxSize(),
        )

        AnimatedVisibility(controlsVisible || state.error != null, enter = fadeIn(), exit = fadeOut()) {
            Row(
                Modifier.fillMaxWidth()
                    .background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = .7f), Color.Transparent)))
                    .safeDrawingPadding()
                    .padding(horizontal = 4.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back", tint = Color.White) }
                Column(Modifier.weight(1f)) {
                    Text(state.item?.title ?: "", color = Color.White, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    val sub = listOfNotNull(state.item?.episodeLabel, state.item?.seriesName, state.plan?.reason).joinToString(" · ")
                    if (sub.isNotEmpty()) Text(sub, color = Color.White.copy(alpha = .7f), style = MaterialTheme.typography.bodySmall, maxLines = 1)
                }
            }
        }

        state.error?.let { message ->
            Column(Modifier.align(Alignment.Center).padding(32.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                Text("Can't play this", color = Color.White, style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.height(4.dp))
                Text(message, color = Color.White.copy(alpha = .75f), style = MaterialTheme.typography.bodyMedium)
                Spacer(Modifier.height(16.dp))
                OutlinedButton(onClick = onBack) { Text("Back") }
            }
        }

        val next = state.upNext
        if (next != null && state.upNextIn != null) {
            Column(
                Modifier.align(Alignment.BottomEnd).safeDrawingPadding().padding(24.dp).widthIn(max = 320.dp)
                    .background(Color.Black.copy(alpha = .8f), RoundedCornerShape(12.dp)).padding(16.dp),
            ) {
                Text("Up next in ${state.upNextIn}s", color = Color.White.copy(alpha = .7f), style = MaterialTheme.typography.labelMedium)
                Text(listOfNotNull(next.episodeLabel, next.title).joinToString(" · "), color = Color.White, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Spacer(Modifier.height(12.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = vm::playNext) { Text("Play now") }
                    OutlinedButton(onClick = vm::cancelUpNext) { Text("Cancel") }
                }
            }
        }
    }
}
