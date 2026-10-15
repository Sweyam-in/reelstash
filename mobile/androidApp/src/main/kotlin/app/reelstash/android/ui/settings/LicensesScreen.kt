package app.reelstash.android.ui.settings

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.TopBarTitle
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.Rs

private const val WEB_LICENSES = "https://sweyam.com/products/reelstash/licenses/"

private data class LicenseDoc(val title: String, val asset: String)

private val DOCS = listOf(
    LicenseDoc("Third-party notice", "licenses/NOTICE.txt"),
    LicenseDoc("Bricolage Grotesque (SIL OFL)", "licenses/bricolage-grotesque-OFL.txt"),
    LicenseDoc("Inter (SIL OFL)", "licenses/inter-OFL.txt"),
)

/** Offline copies of the notices required for fonts and a summary of libraries. */
@Composable
fun LicensesScreen(nav: Navigator) {
    val context = LocalContext.current
    val bodies = remember {
        DOCS.map { doc ->
            doc to runCatching {
                context.assets.open(doc.asset).bufferedReader().use { it.readText() }
            }.getOrElse { "(Could not read ${doc.asset})" }
        }
    }

    Column(Modifier.fillMaxSize()) {
        RsTopBar(nav, onBack = { nav.back() }) { TopBarTitle("Open-source licences") }
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp)) {
            item {
                Text(
                    "ReelStash is GPL v3. Fonts need their OFL text in the app; other libraries are summarised below.",
                    style = MaterialTheme.typography.bodyMedium,
                    color = Rs.colors.textSecondary,
                )
                TextButton(onClick = {
                    context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(WEB_LICENSES)))
                }) { Text("Open on sweyam.com") }
                Spacer(Modifier.height(12.dp))
            }
            items(bodies, key = { it.first.asset }) { (doc, text) ->
                Text(doc.title, style = MaterialTheme.typography.titleMedium)
                Spacer(Modifier.height(8.dp))
                Text(
                    text,
                    style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    color = Rs.colors.textSecondary,
                )
                Spacer(Modifier.height(24.dp))
            }
        }
    }
}
