package app.reelstash.android.lock

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Fingerprint
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.fragment.app.FragmentActivity
import app.reelstash.android.R
import app.reelstash.android.ui.common.GradientButton
import app.reelstash.android.ui.common.ambientGlow
import app.reelstash.android.ui.theme.Rs

/** Shown over everything while the device lock is engaged; asks to unlock straight away. */
@Composable
fun DeviceLockScreen(activity: FragmentActivity, lock: DeviceLock) {
    LaunchedEffect(Unit) { lock.unlock(activity) }
    Column(
        Modifier.fillMaxSize().ambientGlow(Rs.colors.glow).padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Image(painterResource(R.drawable.ic_logo), contentDescription = null, modifier = Modifier.size(84.dp))
        Spacer(Modifier.height(22.dp))
        Text("ReelStash is locked", style = MaterialTheme.typography.headlineMedium)
        Spacer(Modifier.height(8.dp))
        Text(
            "Unlock with your fingerprint, face or screen lock.",
            style = MaterialTheme.typography.bodyMedium, color = Rs.colors.textSecondary, textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(28.dp))
        GradientButton("Unlock", onClick = { lock.unlock(activity) }, icon = Icons.Filled.Fingerprint)
    }
}
