package app.reelstash.android.ui.common

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.BrandGradient
import app.reelstash.android.ui.theme.Rs

/**
 * The bar at the top of each tab: what you are looking at on the left; Search
 * and your account on the right, as on the web.
 */
@Composable
fun RsTopBar(
    nav: Navigator,
    modifier: Modifier = Modifier,
    onBack: (() -> Unit)? = null,
    showSearch: Boolean = true,
    background: Color = Color.Transparent,
    title: @Composable () -> Unit,
) {
    Row(
        modifier.fillMaxWidth().background(background).statusBarsPadding().height(56.dp).padding(horizontal = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (onBack != null) IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
        else Spacer(Modifier.width(6.dp))
        Box(Modifier.weight(1f)) { title() }
        if (showSearch) IconButton(onClick = nav::openSearch) { Icon(Icons.Outlined.Search, "Search", tint = Rs.colors.text) }
        Avatar(onClick = nav::openSettings)
        Spacer(Modifier.width(6.dp))
    }
}

/** A plain text title for [RsTopBar]. */
@Composable
fun TopBarTitle(text: String) {
    Text(text, style = MaterialTheme.typography.titleLarge, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(horizontal = 8.dp))
}

/** The signed-in person's initial in the brand gradient. */
@Composable
fun Avatar(onClick: () -> Unit, size: Int = 34) {
    val name = LocalServer.current.saved.user.displayName.ifBlank { "?" }
    Box(
        Modifier.size(size.dp).clip(CircleShape).background(BrandGradient).clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(name.take(1).uppercase(), color = Color.White, fontWeight = FontWeight.Bold, fontSize = (size * .42).sp)
    }
}
