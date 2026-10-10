package app.reelstash.android.ui.main

import androidx.compose.animation.AnimatedContentTransitionScope
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.Spring
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.spring
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.background
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsPressedAsState
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.navigation.NavBackStackEntry
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountCircle
import androidx.compose.material.icons.filled.Bookmarks
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.SmartDisplay
import androidx.compose.material.icons.filled.VideoLibrary
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Bookmarks
import androidx.compose.material.icons.outlined.Home
import androidx.compose.material.icons.outlined.SmartDisplay
import androidx.compose.material.icons.outlined.VideoLibrary
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Scaffold
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.dp
import androidx.navigation.NavDestination.Companion.hierarchy
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.NavType
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import androidx.navigation.navArgument
import app.reelstash.android.data.ActiveServer
import app.reelstash.android.player.PlayerActivity
import app.reelstash.android.ui.browse.BrowseScreen
import app.reelstash.android.ui.browse.MediaListScreen
import app.reelstash.android.ui.browse.PeopleScreen
import app.reelstash.android.ui.detail.MediaDetailScreen
import app.reelstash.android.ui.detail.PersonScreen
import app.reelstash.android.ui.detail.PlaylistScreen
import app.reelstash.android.ui.detail.SeriesScreen
import app.reelstash.android.ui.home.HomeScreen
import app.reelstash.android.ui.library.LibraryScreen
import app.reelstash.android.ui.reels.ReelsScreen
import app.reelstash.android.ui.search.SearchScreen
import app.reelstash.android.ui.settings.SettingsScreen
import app.reelstash.android.ui.theme.Rs

/** The server the screens below are showing. */
val LocalServer = staticCompositionLocalOf<ActiveServer> { error("No server") }

// Instagram's bar: icon only, outline until selected, then filled. No labels, no pill.
private enum class Tab(val route: String, val label: String, val icon: ImageVector, val selectedIcon: ImageVector) {
    Home("home", "Home", Icons.Outlined.Home, Icons.Filled.Home),
    Browse("browse", "Browse", Icons.Outlined.VideoLibrary, Icons.Filled.VideoLibrary),
    Reels("reels", "Reels", Icons.Outlined.SmartDisplay, Icons.Filled.SmartDisplay),
    Library("library", "Library", Icons.Outlined.Bookmarks, Icons.Filled.Bookmarks),
    Profile("settings", "Profile", Icons.Outlined.AccountCircle, Icons.Filled.AccountCircle),
}

/** Navigation calls the screens make, so they never touch routes directly. */
class Navigator(private val nav: NavHostController, private val play: (Long) -> Unit) {
    fun openMedia(id: Long) = nav.navigate("media/$id")
    fun openSeries(id: Long) = nav.navigate("series/$id")
    fun openPlaylist(id: Long) = nav.navigate("playlist/$id")
    fun openPerson(id: Long) = nav.navigate("person/$id")
    fun openList(kind: String) = nav.navigate("list/$kind")
    fun openSearch() = nav.navigate("search") { launchSingleTop = true }
    fun openPeople() = nav.navigate("people") { launchSingleTop = true }
    fun openBrowse() = tab(Tab.Browse.route)
    fun openSettings() = tab(Tab.Profile.route)
    fun play(id: Long) = play.invoke(id)
    fun back() = nav.popBackStack()

    internal fun tab(route: String) = nav.navigate(route) {
        popUpTo(nav.graph.findStartDestination().id) { saveState = true }
        launchSingleTop = true
        restoreState = true
    }
}

@Composable
fun MainScreen(server: ActiveServer) {
    val context = LocalContext.current
    val nav = rememberNavController()
    val navigator = Navigator(nav) { id -> context.startActivity(
            PlayerActivity.intent(context, id),
            android.app.ActivityOptions.makeCustomAnimation(context, app.reelstash.android.R.anim.player_enter, app.reelstash.android.R.anim.stay).toBundle(),
        ) }
    val entry by nav.currentBackStackEntryAsState()
    val current = entry?.destination
    val onReels = current?.route == Tab.Reels.route
    val c = Rs.colors

    // From another tab, back returns to Home inside the app (a fade) instead of closing the app.
    val offHome = current?.route in tabRoutes && current?.route != Tab.Home.route
    androidx.activity.compose.BackHandler(enabled = offHome) { navigator.tab(Tab.Home.route) }

    CompositionLocalProvider(LocalServer provides server) {
        Scaffold(
            containerColor = c.canvas,
            contentWindowInsets = WindowInsets(0, 0, 0, 0),
            bottomBar = {
                InstagramBar(
                    onReels = onReels,
                    selected = { route -> current?.hierarchy?.any { it.route == route } == true },
                    onSelect = navigator::tab,
                )
            },
        ) { padding ->
            NavHost(
                nav,
                startDestination = Tab.Home.route,
                modifier = Modifier.padding(padding),
                enterTransition = { enter() },
                exitTransition = { exit() },
                popEnterTransition = { popEnter() },
                popExitTransition = { popExit() },
                predictivePopEnterTransition = { popEnter() },
                predictivePopExitTransition = { popExit() },
            ) {
                composable(Tab.Home.route) { HomeScreen(navigator) }
                composable(Tab.Browse.route) { BrowseScreen(navigator) }
                composable(Tab.Reels.route) { ReelsScreen(navigator) }
                composable(Tab.Library.route) { LibraryScreen(navigator) }
                composable(Tab.Profile.route) { SettingsScreen(navigator) }
                composable("search") { SearchScreen(navigator) }
                composable("people") { PeopleScreen(navigator) }
                composable("media/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                    MediaDetailScreen(it.arguments!!.getLong("id"), navigator)
                }
                composable("series/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                    SeriesScreen(it.arguments!!.getLong("id"), navigator)
                }
                composable("playlist/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                    PlaylistScreen(it.arguments!!.getLong("id"), navigator)
                }
                composable("person/{id}", listOf(navArgument("id") { type = NavType.LongType })) {
                    PersonScreen(it.arguments!!.getLong("id"), navigator)
                }
                composable("list/{kind}") { MediaListScreen(it.arguments!!.getString("kind")!!, navigator) }
            }
        }
    }
}

private val tabRoutes = Tab.entries.map { it.route }.toSet()
private val Ease = FastOutSlowInEasing

/** Moving between tabs cross-fades; going deeper slides sideways and back mirrors it exactly. */
private fun NavBackStackEntry.isTab() = destination.route in tabRoutes

private fun AnimatedContentTransitionScope<NavBackStackEntry>.bothTabs() = initialState.isTab() && targetState.isTab()

private const val SLIDE_MS = 320
private val Slide = tween<IntOffset>(SLIDE_MS, easing = Ease)
private val Dim = tween<Float>(SLIDE_MS, easing = Ease)

private fun AnimatedContentTransitionScope<NavBackStackEntry>.enter(): EnterTransition =
    if (bothTabs()) fadeIn(tween(200, 60, Ease)) else slideInHorizontally(Slide) { it }

private fun AnimatedContentTransitionScope<NavBackStackEntry>.exit(): ExitTransition =
    if (bothTabs()) fadeOut(tween(120, easing = Ease)) else slideOutHorizontally(Slide) { -it / 4 } + fadeOut(Dim, targetAlpha = 0.6f)

private fun AnimatedContentTransitionScope<NavBackStackEntry>.popEnter(): EnterTransition =
    if (bothTabs()) fadeIn(tween(200, 60, Ease)) else slideInHorizontally(Slide) { -it / 4 } + fadeIn(Dim, initialAlpha = 0.6f)

private fun AnimatedContentTransitionScope<NavBackStackEntry>.popExit(): ExitTransition =
    if (bothTabs()) fadeOut(tween(120, easing = Ease)) else slideOutHorizontally(Slide) { it }

/** Home, Browse, Reels, Library, Profile — icons only, filled when selected, black over reels. */
@Composable
private fun InstagramBar(onReels: Boolean, selected: (String) -> Boolean, onSelect: (String) -> Unit) {
    val c = Rs.colors
    val bar by animateColorAsState(if (onReels) Color.Black else c.surface, tween(250), label = "bar")
    val tint = if (onReels) Color.White else c.text
    Row(
        Modifier.fillMaxWidth().background(bar).windowInsetsPadding(WindowInsets.navigationBars).height(48.dp),
        horizontalArrangement = Arrangement.SpaceEvenly,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Tab.entries.forEach { tab ->
            val on = selected(tab.route)
            val haptic = LocalHapticFeedback.current
            val source = remember { MutableInteractionSource() }
            val pressed by source.collectIsPressedAsState()
            val scale by animateFloatAsState(
                if (pressed) 0.82f else if (on) 1.08f else 1f,
                spring(Spring.DampingRatioMediumBouncy, Spring.StiffnessMedium),
                label = "tab-scale",
            )
            val color by animateColorAsState(if (on) tint else tint.copy(alpha = 0.62f), tween(200), label = "tab-tint")
            Box(
                Modifier.size(width = 56.dp, height = 48.dp).clickable(source, indication = null) {
                    if (!on) haptic.performHapticFeedback(HapticFeedbackType.ContextClick)
                    onSelect(tab.route)
                },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    if (on) tab.selectedIcon else tab.icon, tab.label, tint = color,
                    modifier = Modifier.size(26.dp).graphicsLayer { scaleX = scale; scaleY = scale },
                )
            }
        }
    }
}
