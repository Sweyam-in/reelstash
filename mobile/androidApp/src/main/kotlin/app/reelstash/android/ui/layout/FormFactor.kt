package app.reelstash.android.ui.layout

import android.content.Context
import android.content.pm.PackageManager
import android.content.res.Configuration
import androidx.compose.foundation.border
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.composed
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.theme.Rs

/**
 * How the app is being used: phone (bottom bar), tablet (side rail, roomier
 * grids), or Android TV (rail, D-pad focus rings, overscan margins, no reels).
 */
enum class FormFactor {
    Phone,
    Tablet,
    Tv,
}

val LocalFormFactor = compositionLocalOf { FormFactor.Phone }

/** Compact phones stay under 600dp; tablets and foldables open wider. */
private const val TABLET_WIDTH_DP = 600

fun Context.isTelevision(): Boolean {
    val ui = resources.configuration.uiMode and Configuration.UI_MODE_TYPE_MASK
    if (ui == Configuration.UI_MODE_TYPE_TELEVISION) return true
    return packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
}

@Composable
fun rememberFormFactor(): FormFactor {
    val context = LocalContext.current
    val width = LocalConfiguration.current.screenWidthDp
    val tv = remember(context) { context.isTelevision() }
    return when {
        tv -> FormFactor.Tv
        width >= TABLET_WIDTH_DP -> FormFactor.Tablet
        else -> FormFactor.Phone
    }
}

/** Horizontal page gutter: phones stay tight; tablets and TVs need air (and TV overscan). */
val FormFactor.gutter: Dp
    get() = when (this) {
        FormFactor.Phone -> 16.dp
        FormFactor.Tablet -> 28.dp
        FormFactor.Tv -> 48.dp
    }

val FormFactor.contentMaxWidth: Dp
    get() = when (this) {
        FormFactor.Phone -> Dp.Unspecified
        FormFactor.Tablet -> 1100.dp
        FormFactor.Tv -> 1400.dp
    }

val FormFactor.posterCardWidth: Dp
    get() = when (this) {
        FormFactor.Phone -> 132.dp
        FormFactor.Tablet -> 160.dp
        FormFactor.Tv -> 180.dp
    }

val FormFactor.landscapeCardWidth: Dp
    get() = when (this) {
        FormFactor.Phone -> 232.dp
        FormFactor.Tablet -> 280.dp
        FormFactor.Tv -> 320.dp
    }

val FormFactor.gridMinCell: Dp
    get() = when (this) {
        FormFactor.Phone -> 110.dp
        FormFactor.Tablet -> 140.dp
        FormFactor.Tv -> 160.dp
    }

val FormFactor.gridMinCellLandscape: Dp
    get() = when (this) {
        FormFactor.Phone -> 170.dp
        FormFactor.Tablet -> 220.dp
        FormFactor.Tv -> 260.dp
    }

val FormFactor.isWide: Boolean
    get() = this != FormFactor.Phone

/**
 * On TV, grow and ring the focused item so the remote has a clear target.
 * No-op on phone and tablet (touch already shows pressed state).
 */
fun Modifier.rsFocusTarget(shape: RoundedCornerShape = RoundedCornerShape(16.dp)): Modifier = composed {
    val form = LocalFormFactor.current
    if (form != FormFactor.Tv) return@composed this
    var focused by remember { mutableStateOf(false) }
    this
        .onFocusChanged { focused = it.isFocused }
        .scale(if (focused) 1.06f else 1f)
        .border(
            width = if (focused) 3.dp else 0.dp,
            color = if (focused) Rs.colors.accent else Color.Transparent,
            shape = shape,
        )
}
