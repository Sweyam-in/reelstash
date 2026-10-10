package app.reelstash.android.ui.home

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import app.reelstash.android.ui.common.CardShape
import app.reelstash.android.ui.common.LibraryPicker
import app.reelstash.android.ui.common.Load
import app.reelstash.android.ui.common.LoadContent
import app.reelstash.android.ui.common.MediaRow
import app.reelstash.android.ui.common.MessageBox
import app.reelstash.android.ui.common.PersonChip
import app.reelstash.android.ui.common.RsTopBar
import app.reelstash.android.ui.common.SectionHeader
import app.reelstash.android.ui.common.cardShapeFor
import app.reelstash.android.ui.common.currentLibrary
import app.reelstash.android.ui.common.rememberLibraries
import app.reelstash.android.ui.common.rememberLoad
import app.reelstash.android.ui.common.valueOrNull
import app.reelstash.android.ui.main.LocalServer
import app.reelstash.android.ui.main.Navigator
import app.reelstash.android.ui.theme.Rs
import app.reelstash.android.ui.theme.sectionTone
import app.reelstash.shared.api.MediaQuery
import app.reelstash.shared.model.HomeSection
import app.reelstash.shared.model.MediaItem
import app.reelstash.shared.model.Person
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import java.time.LocalDate

private data class HomeData(
    val hero: List<MediaItem>,
    val resume: List<MediaItem>,
    val nextUp: List<MediaItem>,
    val fresh: List<MediaItem>,
    val favorites: List<MediaItem>,
    val faces: List<Person>,
    val sections: List<HomeSection>,
)

/** How many slides the home slideshow shows, as on the web. */
private const val HERO_SLIDES = 10

/**
 * Today's slides: titles with a backdrop first, then other artwork, then anything
 * recent — shuffled by date so the slideshow changes daily but not on every visit.
 */
private fun heroPick(pool: List<MediaItem>, recent: List<MediaItem>): List<MediaItem> {
    val day = kotlin.random.Random(LocalDate.now().toEpochDay())
    val withBackdrop = pool.filter { it.backdropImageId != null }.shuffled(day)
    val withArt = pool.filter { it.backdropImageId == null }.shuffled(day)
    val picked = (withBackdrop + withArt).take(HERO_SLIDES)
    val seen = picked.map { it.id }.toSet()
    return picked + recent.filter { it.id !in seen }.take(HERO_SLIDES - picked.size)
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(nav: Navigator) {
    val server = LocalServer.current
    val client = server.client
    val libraries = rememberLibraries()
    val library = currentLibrary(libraries.state.valueOrNull)
    val home = rememberLoad("home:${server.id}:${server.saved.libraryId}") {
        coroutineScope {
            val pool = async { runCatching { client.media(MediaQuery(hasArtwork = true), limit = 100).items }.getOrDefault(emptyList()) }
            val fresh = async { client.media(MediaQuery(), limit = 20).items }
            val resume = async { runCatching { client.continueWatching().items }.getOrDefault(emptyList()) }
            val next = async { runCatching { client.nextUp().items }.getOrDefault(emptyList()) }
            val favorites = async { runCatching { client.media(MediaQuery(favorite = true), limit = 20).items }.getOrDefault(emptyList()) }
            val faces = async {
                runCatching { client.performers(limit = 16).items.map { Person(it.id, it.name, it.profileImageId, job = "${it.mediaCount} titles") } }.getOrDefault(emptyList())
            }
            val sections = async { runCatching { client.home().sections }.getOrDefault(emptyList()) }
            HomeData(heroPick(pool.await(), fresh.await()), resume.await(), next.await(), fresh.await(), favorites.await(), faces.await(), sections.await())
        }
    }
    // Coming back from the player: refresh Continue watching without a spinner.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) {
        var first = true
        lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            if (!first) home.refreshQuietly()
            first = false
        }
    }

    var pulled by remember { mutableStateOf(false) }
    LaunchedEffect(home.state) { if (home.state !is Load.Loading) pulled = false }
    val statusTop = WindowInsets.statusBars.asPaddingValues().calculateTopPadding()
    val heroHeight = (LocalConfiguration.current.screenHeightDp * .62f).dp.coerceIn(380.dp, 560.dp)
    val canvas = Rs.colors.canvas

    Box(Modifier.fillMaxSize()) {
        PullToRefreshBox(isRefreshing = pulled, onRefresh = { pulled = true; home.retry() }, modifier = Modifier.fillMaxSize()) {
            LoadContent(home.state, home::retry, Modifier.padding(top = statusTop + 56.dp)) { data ->
                if (data.hero.isEmpty() && data.fresh.isEmpty()) {
                    MessageBox("Your library is empty", "When titles are added on the server, they land here.")
                    return@LoadContent
                }
                val shape = cardShapeFor(library)
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                    HeroCarousel(data.hero, client.urls, onPlay = { nav.play(it.id) }, onOpen = { nav.openMedia(it.id) }, height = heroHeight, topInset = statusTop)
                    var tone = 0
                    MediaRow("Continue watching", data.resume, client.urls, CardShape.Landscape, { nav.openMedia(it.id) }, eyebrow = "Pick up where you left off", tone = sectionTone(tone++))
                    MediaRow("Next up", data.nextUp, client.urls, CardShape.Landscape, { nav.openMedia(it.id) }, eyebrow = "The next episode", tone = sectionTone(tone++))
                    MediaRow(
                        "Recently added", data.fresh, client.urls, shape, { nav.openMedia(it.id) }, eyebrow = "Fresh arrivals", tone = sectionTone(tone++),
                        onViewAll = nav::openBrowse, kicker = { library?.name ?: "Your library" },
                    )
                    if (data.faces.isNotEmpty()) {
                        val t = sectionTone(tone++)
                        Column(Modifier.padding(vertical = 14.dp)) {
                            SectionHeader(
                                if (library?.isEntertainment == true) "Cast in your library" else "Faces in your library",
                                if (library?.isEntertainment == true) "Actors" else "Performers", t.color,
                                actionLabel = "All", onAction = nav::openPeople,
                            )
                            Spacer(Modifier.height(12.dp))
                            LazyRow(contentPadding = PaddingValues(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                                items(data.faces, key = { it.id }) { p -> PersonChip(p, client.urls) { nav.openPerson(p.id) } }
                            }
                        }
                    }
                    MediaRow("Your favorites", data.favorites, client.urls, shape, { nav.openMedia(it.id) }, eyebrow = "Saved with love", tone = sectionTone(tone++), onViewAll = { nav.openList("favorites") })
                    data.sections.forEach { section ->
                        MediaRow(section.title, section.items, client.urls, shape, { nav.openMedia(it.id) }, eyebrow = section.subtitle ?: section.title, tone = sectionTone(tone++))
                    }
                    Spacer(Modifier.height(32.dp))
                }
            }
        }
        // The bar floats over the slideshow, fading in a little so its text stays legible.
        RsTopBar(nav, background = androidx.compose.ui.graphics.Color.Transparent, modifier = Modifier.background(Brush.verticalGradient(listOf(canvas.copy(alpha = .7f), canvas.copy(alpha = 0f))))) {
            LibraryPicker(libraries.state.valueOrNull)
        }
    }
}
