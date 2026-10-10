package app.reelstash.android.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp
import app.reelstash.android.R
import app.reelstash.shared.model.normalizeColorTheme

// The ReelStash media look, taken from the web app: Spatika's Neelam (dark) and
// Mukta (light) themes, plus the admin-chosen color theme (ui/src/colorThemes.js).
// Light and dark stay a personal choice. Bricolage Grotesque for display type
// and Inter for text. Keep the palettes in step with the web and the iOS app.

@Immutable
data class RsColors(
    val dark: Boolean,
    val canvas: Color,
    val surface: Color,
    val raised: Color,
    val overlay: Color,
    val subtle: Color,
    val text: Color,
    val textSecondary: Color,
    val textTertiary: Color,
    val border: Color,
    val borderStrong: Color,
    val accent: Color,
    val accentText: Color,
    val glow: Color,
) {
    /** Tone text: the tone lifted toward white on dark, pushed toward black on light. */
    fun toneInk(tone: Color): Color = if (dark) lerp(tone, Color.White, .28f) else lerp(tone, Color.Black, .34f)
}

private fun neelam(palette: ColorPalette) = RsColors(
    dark = true,
    canvas = Color(0xFF0C0D11),
    surface = Color(0xFF13141A),
    raised = Color(0xFF191A21),
    overlay = Color(0xFF1D1E26),
    subtle = Color(0xFF17181F),
    text = Color(0xFFECECF1),
    textSecondary = Color(0xFFA2A5B3),
    textTertiary = Color(0xFF7F8291),
    border = Color(0xFF25262F),
    borderStrong = Color(0xFF353744),
    accent = palette.accent,
    accentText = palette.accentText,
    glow = palette.accent,
)

private fun mukta(palette: ColorPalette) = RsColors(
    dark = false,
    canvas = Color(0xFFF7F7F8),
    surface = Color(0xFFFFFFFF),
    raised = Color(0xFFFFFFFF),
    overlay = Color(0xFFFFFFFF),
    subtle = Color(0xFFF3F3F5),
    text = Color(0xFF16171D),
    textSecondary = Color(0xFF585B68),
    textTertiary = Color(0xFF676A78),
    border = Color(0xFFE2E3E8),
    borderStrong = Color(0xFFCDCFD7),
    accent = palette.accentLight,
    accentText = palette.accentTextLight,
    glow = palette.accentText,
)

/** One of the five library palettes the admin chooses. Values match ui/src/colorThemes.js. */
data class ColorPalette(
    val id: String,
    val from: Color,
    val to: Color,
    val accent: Color,
    val accentText: Color,
    val accentLight: Color,
    val accentTextLight: Color,
    val tones: List<Color>,
) {
    val brush: Brush get() = Brush.linearGradient(listOf(from, to))
}

private fun palette(id: String, from: Long, to: Long, accent: Long, accentText: Long, accentLight: Long, accentTextLight: Long, tones: List<Long>) = ColorPalette(
    id, Color(from), Color(to), Color(accent), Color(accentText), Color(accentLight), Color(accentTextLight), tones.map { Color(it) },
)

object Palettes {
    val grove = palette(
        "grove", 0xFFF26B1D, 0xFF14956A, 0xFFF26B1D, 0xFFFFC48A, 0xFFC24E0C, 0xFF9A3D09,
        listOf(0xFFF26B1D, 0xFFFF9A3C, 0xFFF5C330, 0xFF22C998, 0xFF0E9AA7, 0xFF7BC142, 0xFFE8A317, 0xFF3DBE6E),
    )
    val lagoon = palette(
        "lagoon", 0xFF14B8C8, 0xFF2563EB, 0xFF14B8C8, 0xFFA5F3FC, 0xFF0E7490, 0xFF155E75,
        listOf(0xFF14B8C8, 0xFF38BDF8, 0xFF67E8F9, 0xFF2DD4BF, 0xFF2563EB, 0xFF22D3EE, 0xFF60A5FA, 0xFF0EA5E9),
    )
    val ember = palette(
        "ember", 0xFFFF4D2E, 0xFFF5B942, 0xFFFF4D2E, 0xFFFFD0C2, 0xFFC2410C, 0xFF9A3412,
        listOf(0xFFFF4D2E, 0xFFFF7A3C, 0xFFF5B942, 0xFFFF8A4C, 0xFFE85D04, 0xFFF59E0B, 0xFFD97706, 0xFFFB923C),
    )
    val iris = palette(
        "iris", 0xFF7C5CFF, 0xFF3B82F6, 0xFF7C5CFF, 0xFFDDD6FE, 0xFF5B21B6, 0xFF4C1D95,
        listOf(0xFF7C5CFF, 0xFF818CF8, 0xFFA78BFA, 0xFF60A5FA, 0xFF4F46E5, 0xFF38BDF8, 0xFF8B5CF6, 0xFF22D3EE),
    )
    val aurora = palette(
        "aurora", 0xFF2DD4A8, 0xFF6D5EF7, 0xFF2DD4A8, 0xFFBBF7E0, 0xFF0F766E, 0xFF115E59,
        listOf(0xFF2DD4A8, 0xFF5EEAD4, 0xFFA78BFA, 0xFF34D399, 0xFF6D5EF7, 0xFF67E8F9, 0xFF22C55E, 0xFF818CF8),
    )
    private val all = listOf(grove, lagoon, ember, iris, aurora)
    fun byId(id: String?): ColorPalette = all.find { it.id == normalizeColorTheme(id) } ?: grove
}

val LocalPalette = staticCompositionLocalOf { Palettes.grove }

/** The eight spectrum tones of the active palette. */
val Tones: List<Color>
    @Composable @ReadOnlyComposable get() = LocalPalette.current.tones

/** Each tone's partner for two-colour washes, as the web pairs them. */
private val TonePartner = listOf(2, 7, 0, 4, 5, 6, 7, 1)

data class Tone(val color: Color, val partner: Color)

/** The same key always gets the same tone (FNV-1a, as the web's hashString). */
@Composable
@ReadOnlyComposable
fun toneFor(key: Any): Tone {
    val tones = Tones
    var h = 2166136261L
    for (ch in key.toString()) {
        h = h xor ch.code.toLong()
        h = (h * 16777619L) and 0xFFFFFFFFL
    }
    val i = (h % tones.size).toInt()
    return Tone(tones[i], tones[TonePartner[i]])
}

/** Tone used for a section of the home page, in the order the web cycles through. */
@Composable
@ReadOnlyComposable
fun sectionTone(index: Int): Tone {
    val tones = Tones
    return Tone(listOf(tones[4], tones[7], tones[5], tones[2], tones[3], tones[6])[index % 6], tones[5])
}

/** The brand gradient of the active palette, at the web's 120° angle (approximated horizontally). */
val BrandGradient: Brush
    @Composable @ReadOnlyComposable get() = LocalPalette.current.brush

val BrandOrange: Color
    @Composable @ReadOnlyComposable get() = LocalPalette.current.accent

@OptIn(ExperimentalTextApi::class)
private fun bricolage(weight: Int) = Font(
    R.font.bricolage_grotesque,
    FontWeight(weight),
    variationSettings = FontVariation.Settings(FontVariation.weight(weight), FontVariation.Setting("opsz", 48f)),
)

@OptIn(ExperimentalTextApi::class)
private fun inter(weight: Int) = Font(R.font.inter, FontWeight(weight), variationSettings = FontVariation.Settings(FontVariation.weight(weight)))

val DisplayFamily = FontFamily(bricolage(500), bricolage(600), bricolage(700), bricolage(800))
val TextFamily = FontFamily(inter(400), inter(500), inter(600), inter(650), inter(700), inter(800))

private fun typography(): Typography {
    val text = TextStyle(fontFamily = TextFamily)
    val display = TextStyle(fontFamily = DisplayFamily, fontWeight = FontWeight.Bold)
    return Typography(
        displaySmall = display.copy(fontSize = 34.sp, lineHeight = 36.sp, letterSpacing = (-0.035).em),
        headlineLarge = display.copy(fontSize = 30.sp, lineHeight = 33.sp, letterSpacing = (-0.035).em),
        headlineMedium = display.copy(fontSize = 26.sp, lineHeight = 30.sp, letterSpacing = (-0.035).em),
        headlineSmall = display.copy(fontSize = 22.sp, lineHeight = 26.sp, letterSpacing = (-0.025).em),
        titleLarge = display.copy(fontSize = 20.sp, lineHeight = 24.sp, letterSpacing = (-0.02).em),
        titleMedium = text.copy(fontSize = 15.sp, lineHeight = 20.sp, fontWeight = FontWeight(650)),
        titleSmall = text.copy(fontSize = 14.sp, lineHeight = 19.sp, fontWeight = FontWeight(650)),
        bodyLarge = text.copy(fontSize = 16.sp, lineHeight = 24.sp),
        bodyMedium = text.copy(fontSize = 14.sp, lineHeight = 21.sp),
        bodySmall = text.copy(fontSize = 12.5.sp, lineHeight = 17.sp),
        labelLarge = text.copy(fontSize = 14.sp, lineHeight = 18.sp, fontWeight = FontWeight(650)),
        labelMedium = text.copy(fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight(650)),
        labelSmall = text.copy(fontSize = 11.sp, lineHeight = 14.sp, fontWeight = FontWeight(600)),
    )
}

/** The small all-caps label above headings ("FROM YOUR COLLECTION"). */
val EyebrowStyle = TextStyle(fontFamily = TextFamily, fontWeight = FontWeight(800), fontSize = 10.5.sp, letterSpacing = 0.17.em)

/** Card kicker: the short coloured line above a card's title. */
val KickerStyle = TextStyle(fontFamily = TextFamily, fontWeight = FontWeight.Bold, fontSize = 11.5.sp)

val LocalRsColors = staticCompositionLocalOf { neelam(Palettes.grove) }

object Rs {
    val colors: RsColors
        @Composable @ReadOnlyComposable get() = LocalRsColors.current
}

@Composable
fun ReelStashTheme(dark: Boolean = isSystemInDarkTheme(), palette: ColorPalette = LocalPalette.current, content: @Composable () -> Unit) {
    val c = if (dark) neelam(palette) else mukta(palette)
    val scheme = if (dark) {
        darkColorScheme(
            primary = c.accent, onPrimary = Color.White,
            primaryContainer = c.accent.copy(alpha = .16f), onPrimaryContainer = c.accentText,
            secondary = c.accentText, onSecondary = Color.White,
            secondaryContainer = c.accent.copy(alpha = .16f), onSecondaryContainer = c.accentText,
            tertiary = Tones[5],
            background = c.canvas, onBackground = c.text,
            surface = c.canvas, onSurface = c.text,
            surfaceVariant = c.raised, onSurfaceVariant = c.textSecondary,
            surfaceContainerLowest = c.canvas, surfaceContainerLow = c.surface, surfaceContainer = c.surface,
            surfaceContainerHigh = c.raised, surfaceContainerHighest = c.overlay,
            outline = c.borderStrong, outlineVariant = c.border,
            error = Color(0xFFFF8A80), errorContainer = Color(0xFF3A1515), onErrorContainer = Color(0xFFFFB4AB),
            inverseSurface = Color(0xFFEDEDF0), inverseOnSurface = c.surface,
        )
    } else {
        lightColorScheme(
            primary = c.accent, onPrimary = Color.White,
            primaryContainer = c.accent.copy(alpha = .12f), onPrimaryContainer = c.accentText,
            secondary = c.accentText, secondaryContainer = c.accent.copy(alpha = .12f), onSecondaryContainer = c.accentText,
            tertiary = Tones[5],
            background = c.canvas, onBackground = c.text,
            surface = c.canvas, onSurface = c.text,
            surfaceVariant = c.subtle, onSurfaceVariant = c.textSecondary,
            surfaceContainerLowest = c.surface, surfaceContainerLow = c.surface, surfaceContainer = c.surface,
            surfaceContainerHigh = c.subtle, surfaceContainerHighest = Color(0xFFEEEEF1),
            outline = c.borderStrong, outlineVariant = c.border,
            error = Color(0xFFBF2F27),
        )
    }
    CompositionLocalProvider(LocalRsColors provides c, LocalPalette provides palette) {
        MaterialTheme(colorScheme = scheme, typography = typography()) {
            Surface(color = c.canvas, contentColor = c.text, content = content)
        }
    }
}
