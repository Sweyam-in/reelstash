import SwiftUI

/// One library palette. Hex values match ui/src/colorThemes.js and the Android app.
struct ColorPalette: Equatable {
    let id: String
    let accent: Color
    let accentText: Color
    let from: Color
    let to: Color
    let tones: [Color]

    fileprivate static func hex(_ value: UInt32) -> Color {
        Color(
            red: Double((value >> 16) & 0xff) / 255,
            green: Double((value >> 8) & 0xff) / 255,
            blue: Double(value & 0xff) / 255
        )
    }

    fileprivate static func make(_ id: String, _ accent: UInt32, _ accentText: UInt32, _ from: UInt32, _ to: UInt32, _ tones: [UInt32]) -> ColorPalette {
        ColorPalette(id: id, accent: hex(accent), accentText: hex(accentText), from: hex(from), to: hex(to), tones: tones.map(hex))
    }
}

enum ColorPalettes {
    static let grove = ColorPalette.make("grove", 0xF26B1D, 0xFFC48A, 0xF26B1D, 0x14956A, [0xF26B1D, 0xFF9A3C, 0xF5C330, 0x22C998, 0x0E9AA7, 0x7BC142, 0xE8A317, 0x3DBE6E])
    static let lagoon = ColorPalette.make("lagoon", 0x14B8C8, 0xA5F3FC, 0x14B8C8, 0x2563EB, [0x14B8C8, 0x38BDF8, 0x67E8F9, 0x2DD4BF, 0x2563EB, 0x22D3EE, 0x60A5FA, 0x0EA5E9])
    static let ember = ColorPalette.make("ember", 0xFF4D2E, 0xFFD0C2, 0xFF4D2E, 0xF5B942, [0xFF4D2E, 0xFF7A3C, 0xF5B942, 0xFF8A4C, 0xE85D04, 0xF59E0B, 0xD97706, 0xFB923C])
    static let iris = ColorPalette.make("iris", 0x7C5CFF, 0xDDD6FE, 0x7C5CFF, 0x3B82F6, [0x7C5CFF, 0x818CF8, 0xA78BFA, 0x60A5FA, 0x4F46E5, 0x38BDF8, 0x8B5CF6, 0x22D3EE])
    static let aurora = ColorPalette.make("aurora", 0x2DD4A8, 0xBBF7E0, 0x2DD4A8, 0x6D5EF7, [0x2DD4A8, 0x5EEAD4, 0xA78BFA, 0x34D399, 0x6D5EF7, 0x67E8F9, 0x22C55E, 0x818CF8])

    static func resolve(_ id: String?) -> ColorPalette {
        switch id {
        case "lagoon": lagoon
        case "ember": ember
        case "iris": iris
        case "aurora": aurora
        default: grove
        }
    }
}

/// Neelam, the dark theme shared with the web and Android apps.
/// Accent colours follow the admin-chosen palette held in `Rs.palette`.
enum Rs {
    static var palette = ColorPalettes.grove
    static let canvas = Color(red: 0.047, green: 0.051, blue: 0.067)
    static let surface = Color(red: 0.075, green: 0.078, blue: 0.102)
    static let text = Color(red: 0.925, green: 0.925, blue: 0.945)
    static let secondary = Color(red: 0.635, green: 0.647, blue: 0.702)
    static let tertiary = Color(red: 0.498, green: 0.510, blue: 0.569)
    static var accent: Color { palette.accent }
    static var accentText: Color { palette.accentText }
    static var orange: Color { palette.from }
    static var green: Color { palette.to }
    static var tones: [Color] { palette.tones }

    static func tone(_ index: Int) -> Color { tones[index % tones.count] }

    static func display(_ size: CGFloat) -> Font {
        .custom("Bricolage Grotesque", size: size, relativeTo: .largeTitle).weight(.bold)
    }

    static func text(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .custom("Inter", size: size, relativeTo: .body).weight(weight)
    }
}

struct RsBackground: ViewModifier {
    func body(content: Content) -> some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .background(Rs.canvas.ignoresSafeArea())
            .foregroundStyle(Rs.text)
            .tint(Rs.accent)
    }
}

extension View {
    func rsScreen() -> some View { modifier(RsBackground()) }

    @ViewBuilder
    func rsGlass(in shape: some Shape = .rect(cornerRadius: 22), interactive: Bool = true) -> some View {
        if interactive {
            glassEffect(.regular.interactive(), in: shape)
        } else {
            glassEffect(.regular, in: shape)
        }
    }
}

extension Button {
    /// Orange liquid-glass primary, the brand stand-in for a flat filled button.
    func rsProminent() -> some View {
        buttonStyle(.glassProminent)
            .tint(Rs.accent)
            .font(Rs.text(17, weight: .semibold))
    }

    func rsGlassControl() -> some View {
        buttonStyle(.glass)
            .font(Rs.text(17, weight: .semibold))
    }
}

struct ShelfHeader: View {
    let eyebrow: String
    let title: String
    var tone: Color = Rs.accentText

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(eyebrow)
                .font(Rs.text(11, weight: .bold))
                .tracking(1.6)
                .textCase(.uppercase)
                .foregroundStyle(tone)
            Text(title)
                .font(Rs.display(22))
                .foregroundStyle(Rs.text)
        }
    }
}

struct BrandField: ViewModifier {
    func body(content: Content) -> some View {
        content
            .font(Rs.text(17))
            .padding(14)
            .rsGlass(in: .rect(cornerRadius: 16))
    }
}

extension View {
    func brandField() -> some View { modifier(BrandField()) }
}
