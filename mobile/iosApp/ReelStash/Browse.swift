import AVFoundation
import Shared
import SwiftUI

struct HomeView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var sidebarCollapsed = false
    var openSearch: () -> Void = {}
    var open: (MediaRoute) -> Void
    var play: (PlayRequest) -> Void
    var select: (SideLink) -> Void = { _ in }

    @State private var bundle: HomeBundle?
    @State private var slide = 0
    @State private var error: String?
    @State private var loading = true

    private var gutter: CGFloat { size == .regular ? 36 : 20 }

    var body: some View {
        ZStack(alignment: .top) {
            scroll
            homeChrome
        }
        .toolbar(.hidden, for: .navigationBar)
        .task(id: model.libraryId) { await load() }
        .task(id: slides.map(\.id)) { await advanceSlides() }
    }

    private var scroll: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: size == .regular ? 36 : 28) {
                if loading && bundle == nil {
                    ProgressView()
                        .controlSize(.large)
                        .tint(Rs.accent)
                        .frame(maxWidth: .infinity)
                        .padding(.top, 160)
                }
                if let error, bundle == nil, !loading {
                    Text(error)
                        .font(Rs.text(16))
                        .foregroundStyle(Rs.accentText)
                        .padding(.horizontal, gutter)
                        .padding(.top, 80)
                }
                if let hero = slides[safe: slide] ?? slides.first {
                    heroCard(hero, count: slides.count)
                } else if !loading {
                    emptyHome
                }
                shelf("Pick up where you left off", "Continue watching", items: bundle?.resume ?? [], shape: .landscape, tone: 0)
                shelf("Your next episodes", "Next up", items: bundle?.nextUp ?? [], shape: .landscape, tone: 3)
                shelf("Fresh arrivals", "Recently added", items: bundle?.fresh ?? [], shape: .poster, tone: 4)
                shelf("Saved with love", "Your favorites", items: bundle?.favorites ?? [], shape: .poster, tone: 1)
                peopleShelf
                ForEach(Array((bundle?.sections ?? []).enumerated()), id: \.element.key) { index, section in
                    shelf(section.subtitle ?? section.title, section.title, items: section.items, shape: .poster, tone: index + 2)
                }
            }
            .padding(.bottom, 28)
        }
        .scrollClipDisabled()
        .contentMargins(.top, 0, for: .scrollContent)
        .ignoresSafeArea(edges: .top)
        .refreshable { await load() }
    }

    private var slides: [MediaItem] {
        let art = Array(bundle?.slides ?? [])
        let seen = Set(art.map(\.id))
        let extra = Array(bundle?.fresh ?? []).filter { !seen.contains($0.id) }
        return Array((art + extra).prefix(10))
    }

    private var emptyHome: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Nothing on the home page yet")
                .font(Rs.display(32))
            Text(error ?? "Titles, continue watching, and suggestions show up here once this library has them.")
                .font(Rs.text(16))
                .foregroundStyle(Rs.secondary)
        }
        .padding(.horizontal, gutter)
        .padding(.top, 80)
    }

    private var peopleShelf: some View {
        let people = Array(bundle?.people ?? [])
        return Group {
            if !people.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    ShelfHeader(eyebrow: model.entertainment ? "Actors" : "Performers", title: "Faces in your library", tone: Rs.tone(5))
                        .padding(.horizontal, gutter)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 16) {
                            ForEach(people, id: \.id) { person in
                                Button { open(.person(person.id)) } label: {
                                    VStack(spacing: 8) {
                                        AuthImage(
                                            url: person.appleImageId() >= 0 ? URL(string: model.session?.performerUrl(id: person.id) ?? "") : nil,
                                            headers: model.headers(),
                                            initial: person.name
                                        )
                                        .frame(width: size == .regular ? 120 : 88, height: size == .regular ? 120 : 88)
                                        .clipShape(Circle())
                                        Text(person.name).font(Rs.text(13, weight: .medium)).lineLimit(2).multilineTextAlignment(.center)
                                            .frame(width: size == .regular ? 120 : 88)
                                    }
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, gutter)
                    }
                    .scrollClipDisabled()
                }
            }
        }
    }

    private func advanceSlides() async {
        guard slides.count > 1 else { return }
        while !Task.isCancelled {
            try? await Task.sleep(for: .seconds(7))
            guard !Task.isCancelled, slides.count > 1 else { return }
            slide = (slide + 1) % slides.count
        }
    }

    private func heroCard(_ item: MediaItem, count: Int) -> some View {
        let height: CGFloat = size == .regular ? 560 : 460
        return ZStack(alignment: .bottomLeading) {
            AuthImage(
                url: backdropURL(item, session: model.session) ?? posterURL(item, session: model.session),
                fallback: thumbnailURL(item, session: model.session),
                headers: model.headers(),
                initial: item.title
            )
            .frame(height: height)
            .frame(maxWidth: .infinity)
            LinearGradient(
                stops: [
                    .init(color: .clear, location: 0.25),
                    .init(color: Rs.canvas.opacity(0.35), location: 0.55),
                    .init(color: Rs.canvas, location: 1),
                ],
                startPoint: .top,
                endPoint: .bottom
            )
            VStack(alignment: .leading, spacing: 14) {
                if !item.appleMeta().isEmpty {
                    Text(item.appleMeta())
                        .font(Rs.text(12, weight: .bold))
                        .tracking(1.5)
                        .textCase(.uppercase)
                        .foregroundStyle(Rs.accentText)
                }
                Text(item.title)
                    .font(Rs.display(size == .regular ? 64 : 40))
                    .lineLimit(2)
                    .shadow(color: .black.opacity(0.45), radius: 12, y: 4)
                GlassEffectContainer(spacing: 12) {
                    HStack(spacing: 12) {
                        Button {
                            play(PlayRequest(id: item.id, title: item.title, resume: resume(item)))
                        } label: {
                            Label(item.appleProgress() >= 0 ? "Resume" : "Play", systemImage: "play.fill")
                                .padding(.horizontal, 8)
                        }
                        .rsProminent()
                        Button { open(.media(item.id)) } label: {
                            Label("Details", systemImage: "info.circle")
                                .padding(.horizontal, 6)
                        }
                        .rsGlassControl()
                        if count > 1 {
                            Button { slide = (slide + count - 1) % count } label: { Image(systemName: "chevron.left") }
                                .rsGlassControl()
                            Text("\(slide + 1) / \(count)")
                                .font(Rs.text(13, weight: .semibold))
                                .foregroundStyle(Rs.secondary)
                            Button { slide = (slide + 1) % count } label: { Image(systemName: "chevron.right") }
                                .rsGlassControl()
                        }
                    }
                }
            }
            .padding(.horizontal, gutter)
            .padding(.bottom, 4)
        }
    }

    private var homeChrome: some View {
        HStack(spacing: 10) {
            if sidebarCollapsed {
                HStack(spacing: 2) {
                    ForEach(SideLink.topBar, id: \.self) { item in
                        Button { select(item) } label: {
                            Text(item.title)
                                .font(Rs.text(15, weight: item == .home ? .semibold : .regular))
                                .padding(.horizontal, 12)
                                .padding(.vertical, 7)
                                .background(item == .home ? AnyShapeStyle(.white.opacity(0.16)) : AnyShapeStyle(.clear), in: Capsule())
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(4)
                .rsGlass(in: Capsule())
            }
            Spacer(minLength: 0)
            Button(action: openSearch) {
                Image(systemName: "magnifyingglass")
                    .font(.body.weight(.semibold))
                    .frame(width: 36, height: 36)
            }
            .rsGlassControl()
        }
        .padding(.horizontal, gutter)
        .padding(.top, 8)
    }

    private func shelf(_ eyebrow: String, _ title: String, items: [MediaItem], shape: ShelfShape, tone: Int) -> some View {
        Group {
            if !items.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    ShelfHeader(eyebrow: eyebrow, title: title, tone: Rs.tone(tone))
                        .padding(.horizontal, gutter)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(alignment: .top, spacing: shape == .landscape ? 16 : 14) {
                            ForEach(items, id: \.id) { item in
                                PosterCard(item: item, shape: shape, width: cardWidth(shape)) { open(.media(item.id)) }
                            }
                        }
                        .padding(.horizontal, gutter)
                    }
                    .scrollClipDisabled()
                }
            }
        }
    }

    private func cardWidth(_ shape: ShelfShape) -> CGFloat {
        switch (shape, size) {
        case (.landscape, .regular): 320
        case (.landscape, _): 240
        case (.poster, .regular): 168
        default: 128
        }
    }

    private func load() async {
        guard let session = model.session else {
            loading = false
            error = "Not signed in."
            return
        }
        do {
            bundle = try await awaitCall { session.loadHome(onOk: $0, onErr: $1) }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        loading = false
    }
}

extension Array {
    subscript(safe index: Int) -> Element? {
        indices.contains(index) ? self[index] : nil
    }
}

struct MediaGridView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var title: String
    var kind: String = ""
    var favorite = false
    var watchLater = false
    var open: (MediaRoute) -> Void
    @State private var items: [MediaItem] = []
    @State private var total = 0
    @State private var loadingMore = false
    @State private var error: String?

    private let pageSize = 60

    var body: some View {
        ScrollView {
            if let error, items.isEmpty { Text(error).foregroundStyle(Rs.accentText).padding() }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 160 : 110), spacing: 16)], spacing: 16) {
                ForEach(items, id: \.id) { item in
                    PosterCard(item: item) { open(.media(item.id)) }
                        .onAppear {
                            if item.id == items.dropLast(6).last?.id || item.id == items.last?.id {
                                Task { await loadMore() }
                            }
                        }
                }
            }
            .padding(size == .regular ? 28 : 16)
            if loadingMore {
                ProgressView().tint(Rs.accent).padding(.bottom, 24)
            }
        }
        .navigationTitle(title)
        .toolbarTitleDisplayMode(.inlineLarge)
        .task(id: "\(kind)-\(favorite)-\(watchLater)-\(model.libraryId)") {
            items = []
            total = 0
            await loadMore()
        }
    }

    private func loadMore() async {
        guard let session = model.session, !loadingMore else { return }
        if total > 0, items.count >= total { return }
        loadingMore = true
        defer { loadingMore = false }
        let offset = items.count
        do {
            let page = try await awaitCall {
                session.media(kind: kind, sort: "", favorite: favorite, watchLater: watchLater, reels: false, text: "", seriesId: -1, performerId: -1, limit: Int32(pageSize), offset: Int32(offset), onOk: $0, onErr: $1)
            }
            let seen = Set(items.map(\.id))
            items.append(contentsOf: Array(page.items).filter { !seen.contains($0.id) })
            total = Int(page.total)
            if page.items.isEmpty { total = items.count }
            error = nil
        } catch {
            if items.isEmpty { self.error = error.localizedDescription }
        }
    }
}

struct SeriesGridView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var title: String
    var shows: Bool
    var open: (MediaRoute) -> Void
    @State private var items: [Series] = []
    @State private var total = 0
    @State private var loading = false
    @State private var error: String?
    @State private var query = ""

    private let pageSize = 48
    private var gutter: CGFloat { size == .regular ? 28 : 16 }
    private var shown: [Series] {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return items }
        return items.filter { $0.name.localizedStandardContains(text) }
    }

    var body: some View {
        ScrollView {
            if loading && items.isEmpty {
                catalogSkeleton(ratio: 16 / 9, minimum: size == .regular ? 320 : 260)
            } else if let error, items.isEmpty {
                CatalogMessage(symbol: "exclamationmark.triangle", title: "Series didn’t load", message: error)
            } else if items.isEmpty {
                CatalogMessage(symbol: "rectangle.stack", title: "No series yet", message: "Shows and series from this library will collect here.")
            } else if shown.isEmpty {
                CatalogMessage(symbol: "magnifyingglass", title: "Nothing matches", message: "Try another name.")
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 320 : 260), spacing: 18)], spacing: 18) {
                    ForEach(shown, id: \.id) { series in
                        SeriesCard(series: series) { open(.series(series.id)) }
                            .onAppear {
                                guard query.isEmpty, series.id == items.dropLast(4).last?.id || series.id == items.last?.id else { return }
                                Task { await loadMore() }
                            }
                    }
                }
                if loading {
                    ProgressView().tint(Rs.accent).frame(maxWidth: .infinity).padding(.top, 8)
                }
            }
        }
        .contentMargins(.horizontal, gutter, for: .scrollContent)
        .contentMargins(.vertical, gutter, for: .scrollContent)
        .navigationTitle(title)
        .navigationSubtitle(total > 0 ? "\(total)" : "")
        .toolbarTitleDisplayMode(.inlineLarge)
        .searchable(text: $query, prompt: "Find a series")
        .refreshable {
            items = []
            total = 0
            await loadMore()
        }
        .task(id: "\(shows)-\(model.libraryId)") {
            items = []
            total = 0
            await loadMore()
        }
    }

    private func loadMore() async {
        guard let session = model.session, !loading else { return }
        if total > 0, items.count >= total { return }
        loading = true
        defer { loading = false }
        let offset = items.count
        do {
            let page: SeriesPage = if shows {
                try await awaitCall { session.shows(limit: Int32(pageSize), offset: Int32(offset), onOk: $0, onErr: $1) }
            } else {
                try await awaitCall { session.seriesList(limit: Int32(pageSize), offset: Int32(offset), onOk: $0, onErr: $1) }
            }
            let seen = Set(items.map(\.id))
            items.append(contentsOf: Array(page.items).filter { !seen.contains($0.id) })
            total = Int(page.total)
            if page.items.isEmpty { total = items.count }
            error = nil
        } catch {
            if items.isEmpty { self.error = error.localizedDescription }
        }
    }
}

struct SeriesCard: View {
    @EnvironmentObject private var model: AppModel
    let series: Series
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            Color.clear
                .aspectRatio(16 / 9, contentMode: .fit)
                .overlay { art }
                .overlay(alignment: .bottomLeading) {
                    LinearGradient(colors: [.clear, .black.opacity(0.2), .black.opacity(0.82)], startPoint: .top, endPoint: .bottom)
                }
                .overlay(alignment: .bottomLeading) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(series.name)
                            .font(Rs.display(22))
                            .lineLimit(2)
                            .shadow(color: .black.opacity(0.45), radius: 8, y: 2)
                        Text(seriesMeta)
                            .font(Rs.text(13, weight: .medium))
                            .foregroundStyle(.white.opacity(0.82))
                            .lineLimit(1)
                    }
                    .padding(16)
                }
                .overlay(alignment: .topTrailing) {
                    Text(series.mediaCount == 1 ? "1 title" : "\(series.mediaCount) titles")
                        .font(Rs.text(11, weight: .bold))
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .rsGlass(in: Capsule(), interactive: false)
                        .padding(12)
                }
                .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 22, style: .continuous)
                        .strokeBorder(.white.opacity(0.12), lineWidth: 1)
                }
        }
        .buttonStyle(RsPressStyle())
    }

    private var art: some View {
        AuthImage(
            url: seriesBackdropURL(series, session: model.session) ?? seriesPosterURL(series, session: model.session),
            fallback: seriesPosterURL(series, session: model.session),
            headers: model.headers(),
            initial: series.name
        )
    }

    private var seriesMeta: String {
        var parts: [String] = []
        if series.seasonCount > 0 {
            parts.append(series.seasonCount == 1 ? "1 season" : "\(series.seasonCount) seasons")
        }
        if let studio = series.studioName, !studio.isEmpty { parts.append(studio) }
        if parts.isEmpty { return series.mediaCount == 1 ? "1 title" : "\(series.mediaCount) titles" }
        return parts.joined(separator: " · ")
    }
}

struct PeopleGridView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var title: String
    var open: (MediaRoute) -> Void
    @State private var people: [Performer] = []
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let error { Text(error).foregroundStyle(Rs.accentText).padding() }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 140 : 100), spacing: 16)], spacing: 18) {
                ForEach(people, id: \.id) { person in
                    Button { open(.person(person.id)) } label: {
                        VStack(spacing: 8) {
                            AuthImage(url: person.appleImageId() >= 0 ? URL(string: model.session?.performerUrl(id: person.id) ?? "") : nil, headers: model.headers(), initial: person.name)
                                .aspectRatio(1, contentMode: .fit)
                                .clipShape(Circle())
                            Text(person.name).font(Rs.text(13, weight: .medium)).lineLimit(2).multilineTextAlignment(.center)
                        }
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(size == .regular ? 28 : 16)
        }
        .navigationTitle(title)
        .toolbarTitleDisplayMode(.inlineLarge)
        .task(id: model.libraryId) { await load() }
    }

    private func load() async {
        guard let session = model.session else { return }
        do {
            people = try await awaitCall { session.performers(limit: 80, offset: 0, text: "", onOk: $0, onErr: $1) }.items
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct PlaylistListView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var open: (MediaRoute) -> Void
    @State private var playlists: [Collection] = []
    @State private var loading = true
    @State private var error: String?
    @State private var query = ""

    private var gutter: CGFloat { size == .regular ? 28 : 16 }
    private var shown: [Collection] {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        let ordered = playlists.sorted { lhs, rhs in
            if lhs.pinned != rhs.pinned { return lhs.pinned && !rhs.pinned }
            return lhs.name.localizedStandardCompare(rhs.name) == .orderedAscending
        }
        guard !text.isEmpty else { return ordered }
        return ordered.filter { $0.name.localizedStandardContains(text) }
    }

    var body: some View {
        ScrollView {
            if loading && playlists.isEmpty {
                catalogSkeleton(ratio: 4 / 3, minimum: size == .regular ? 240 : 160)
            } else if let error, playlists.isEmpty {
                CatalogMessage(symbol: "exclamationmark.triangle", title: "Playlists didn’t load", message: error)
            } else if playlists.isEmpty {
                CatalogMessage(symbol: "list.bullet.rectangle", title: "No playlists yet", message: "Collections you make on the web show up here.")
            } else if shown.isEmpty {
                CatalogMessage(symbol: "magnifyingglass", title: "Nothing matches", message: "Try another name.")
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 240 : 160), spacing: 16)], spacing: 16) {
                    ForEach(shown, id: \.id) { list in
                        PlaylistCard(list: list) { open(.playlist(list.id)) }
                    }
                }
            }
        }
        .contentMargins(.horizontal, gutter, for: .scrollContent)
        .contentMargins(.vertical, gutter, for: .scrollContent)
        .navigationTitle("Playlists")
        .navigationSubtitle(playlists.isEmpty ? "" : "\(playlists.count)")
        .toolbarTitleDisplayMode(.inlineLarge)
        .searchable(text: $query, prompt: "Find a playlist")
        .refreshable { await load() }
        .task(id: model.libraryId) { await load() }
    }

    private func load() async {
        guard let session = model.session else {
            loading = false
            return
        }
        loading = true
        defer { loading = false }
        do {
            playlists = Array(try await awaitCall { session.playlists(onOk: $0, onErr: $1) }.items)
            error = nil
        } catch {
            if playlists.isEmpty { self.error = error.localizedDescription }
        }
    }
}

struct PlaylistCard: View {
    @EnvironmentObject private var model: AppModel
    let list: Collection
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            Color.clear
                .aspectRatio(4 / 3, contentMode: .fit)
                .overlay { PlaylistMosaic(covers: Array(list.covers).prefix(4).map(\.id), name: list.name) }
                .overlay(alignment: .bottom) {
                    LinearGradient(stops: [
                        .init(color: .clear, location: 0),
                        .init(color: .black.opacity(0.55), location: 0.45),
                        .init(color: .black.opacity(0.88), location: 1),
                    ], startPoint: .top, endPoint: .bottom)
                    .frame(height: 110)
                    .frame(maxHeight: .infinity, alignment: .bottom)
                }
                .overlay(alignment: .bottomLeading) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(list.name)
                            .font(Rs.display(20))
                            .lineLimit(2)
                            .shadow(color: .black.opacity(0.4), radius: 6, y: 2)
                        HStack(spacing: 6) {
                            if list.smart {
                                Label("Smart", systemImage: "sparkles")
                            }
                            Text(list.mediaCount == 1 ? "1 title" : "\(list.mediaCount) titles")
                        }
                        .font(Rs.text(12, weight: .semibold))
                        .foregroundStyle(.white.opacity(0.82))
                    }
                    .padding(14)
                }
                .overlay(alignment: .topLeading) {
                    if list.pinned {
                        Image(systemName: "pin.fill")
                            .font(.caption.weight(.bold))
                            .padding(7)
                            .rsGlass(in: Circle(), interactive: false)
                            .padding(10)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: 22, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 22, style: .continuous)
                        .strokeBorder(list.pinned ? Rs.accent.opacity(0.7) : .white.opacity(0.12), lineWidth: 1)
                }
        }
        .buttonStyle(RsPressStyle())
    }
}

struct PlaylistMosaic: View {
    @EnvironmentObject private var model: AppModel
    let covers: [Int64]
    let name: String

    var body: some View {
        let tiles = Array(covers.prefix(4))
        Group {
            if tiles.isEmpty {
                ZStack {
                    LinearGradient(colors: [Rs.accent.opacity(0.55), Rs.surface], startPoint: .topLeading, endPoint: .bottomTrailing)
                    Image(systemName: "list.bullet.rectangle")
                        .font(.system(size: 34, weight: .light))
                        .foregroundStyle(.white.opacity(0.92))
                }
            } else if tiles.count == 1 {
                cover(tiles[0])
            } else if tiles.count == 2 {
                HStack(spacing: 2) { cover(tiles[0]); cover(tiles[1]) }
            } else {
                VStack(spacing: 2) {
                    HStack(spacing: 2) { cover(tiles[0]); cover(tiles[1]) }
                    HStack(spacing: 2) {
                        cover(tiles[2])
                        if tiles.count > 3 { cover(tiles[3]) } else { Rs.surface }
                    }
                }
            }
        }
        .accessibilityLabel(name)
    }

    private func cover(_ id: Int64) -> some View {
        AuthImage(
            url: URL(string: model.session?.posterUrl(mediaId: id) ?? ""),
            fallback: URL(string: model.session?.thumbnailUrl(mediaId: id) ?? ""),
            headers: model.headers(),
            initial: name
        )
    }
}

struct BrowseView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    var open: (MediaRoute) -> Void

    @State private var section = 0
    @State private var items: [MediaItem] = []
    @State private var shows: [Series] = []
    @State private var people: [Performer] = []
    @State private var error: String?
    @State private var loading = false

    private var sections: [String] {
        model.entertainment ? ["Movies", "Shows", "People"] : ["Videos", "Series", "People"]
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                Picker("Section", selection: $section) {
                    ForEach(sections.indices, id: \.self) { Text(sections[$0]).tag($0) }
                }
                .pickerStyle(.segmented)
                if let error { Text(error).foregroundStyle(Rs.accent) }
                if section == 2 {
                    peopleGrid
                } else if section == 1 {
                    seriesGrid
                } else {
                    mediaGrid
                }
                if loading { ProgressView() }
            }
            .padding(size == .regular ? 28 : 16)
        }
        .navigationTitle("Browse")
        .toolbarTitleDisplayMode(.inlineLarge)
        .task(id: "\(section)-\(model.libraryId)") { await load() }
    }

    private var mediaGrid: some View {
        LazyVGrid(columns: columns, spacing: 16) {
            ForEach(items, id: \.id) { item in
                PosterCard(item: item) { open(.media(item.id)) }
            }
        }
    }

    private var seriesGrid: some View {
        LazyVGrid(columns: columns, spacing: 16) {
            ForEach(shows, id: \.id) { series in
                Button { open(.series(series.id)) } label: {
                    VStack(alignment: .leading, spacing: 6) {
                        AuthImage(url: series.applePosterId() >= 0 ? URL(string: model.session?.seriesUrl(id: series.id) ?? "") : nil, headers: model.headers(), initial: series.name)
                            .aspectRatio(2 / 3, contentMode: .fit)
                            .clipShape(RoundedRectangle(cornerRadius: 12))
                        Text(series.name).font(.subheadline.weight(.medium)).lineLimit(2)
                        Text("\(series.mediaCount) titles").font(.caption).foregroundStyle(Rs.tertiary)
                    }
                }
                .buttonStyle(.plain)
            }
        }
    }

    private var peopleGrid: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 140 : 104), spacing: 16)], spacing: 16) {
            ForEach(people, id: \.id) { person in
                Button { open(.person(person.id)) } label: {
                    VStack(spacing: 8) {
                        AuthImage(url: person.appleImageId() >= 0 ? URL(string: model.session?.performerUrl(id: person.id) ?? "") : nil, headers: model.headers(), initial: person.name)
                            .aspectRatio(1, contentMode: .fit)
                            .clipShape(Circle())
                        Text(person.name).font(.subheadline).lineLimit(2).multilineTextAlignment(.center)
                    }
                }
                .buttonStyle(.plain)
            }
        }
    }

    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: size == .regular ? 180 : 120), spacing: 18)]
    }

    private func load() async {
        guard let session = model.session else { return }
        loading = true
        error = nil
        defer { loading = false }
        do {
            if section == 2 {
                let page = try await awaitCall { session.performers(limit: 80, offset: 0, text: "", onOk: $0, onErr: $1) }
                people = page.items
            } else if section == 1 {
                let page: SeriesPage
                if model.entertainment {
                    page = try await awaitCall { session.shows(limit: 80, offset: 0, onOk: $0, onErr: $1) }
                } else {
                    page = try await awaitCall { session.seriesList(limit: 80, offset: 0, onOk: $0, onErr: $1) }
                }
                shows = page.items
            } else {
                let kind = model.entertainment ? "movie" : ""
                let page = try await awaitCall {
                    session.media(kind: kind, sort: "", favorite: false, watchLater: false, reels: false, text: "", seriesId: -1, performerId: -1, limit: 80, offset: 0, onOk: $0, onErr: $1)
                }
                items = page.items
            }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct ReelsView: View {
    @EnvironmentObject private var model: AppModel
    var play: (PlayRequest) -> Void
    var open: (MediaRoute) -> Void
    @StateObject private var playback = ReelPlayback()
    @State private var items: [MediaItem] = []
    @State private var current: Int64?
    @State private var total = 0
    @State private var loading = false
    @State private var error: String?
    @State private var seed = UUID().uuidString

    var body: some View {
        Group {
            if items.isEmpty {
                VStack(spacing: 10) {
                    if loading {
                        ProgressView().tint(.white)
                    } else {
                        Image(systemName: "play.rectangle.on.rectangle")
                            .font(.system(size: 36))
                        Text(error ?? "No reels yet")
                            .font(Rs.display(28))
                        Text(error == nil ? "Vertical videos in this library show up here." : "Pull to try again.")
                            .font(Rs.text(15))
                            .foregroundStyle(.white.opacity(0.7))
                    }
                }
                .foregroundStyle(.white)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ScrollView(.vertical) {
                    LazyVStack(spacing: 0) {
                        ForEach(items, id: \.id) { item in
                            reelPage(item)
                                .containerRelativeFrame([.horizontal, .vertical])
                                .id(item.id)
                        }
                    }
                    .scrollTargetLayout()
                }
                .scrollTargetBehavior(.paging)
                .scrollPosition(id: $current)
                .scrollIndicators(.hidden)
            }
        }
        .background(.black)
        .ignoresSafeArea()
        .toolbar(.hidden, for: .navigationBar)
        .task(id: model.libraryId) {
            items = []
            total = 0
            current = nil
            seed = UUID().uuidString
            await loadMore()
        }
        .onChange(of: current) { _, id in
            guard let id, let item = items.first(where: { $0.id == id }) else { return }
            Task { await playback.play(item: item, session: model.session, headers: model.headers()) }
            if let index = items.firstIndex(where: { $0.id == id }), index >= items.count - 3 {
                Task { await loadMore() }
            }
        }
        .onDisappear { playback.stop() }
    }

    private func reelPage(_ item: MediaItem) -> some View {
        let active = current == item.id
        return GeometryReader { geo in
            let videoWidth = min(geo.size.width, geo.size.height * 9 / 16)
            ZStack {
                Color.black
                VStack(spacing: 0) {
                    ZStack {
                        AuthImage(
                            url: thumbnailURL(item, session: model.session),
                            headers: model.headers(),
                            initial: item.title
                        )
                        if active {
                            ReelPlayerView(player: playback.player)
                            if playback.paused {
                                Image(systemName: "play.fill")
                                    .font(.system(size: 44))
                                    .foregroundStyle(.white.opacity(0.9))
                                    .shadow(radius: 8)
                            }
                            if let message = playback.error {
                                Text(message)
                                    .font(Rs.text(14, weight: .medium))
                                    .multilineTextAlignment(.center)
                                    .padding(14)
                                    .background(.black.opacity(0.55), in: RoundedRectangle(cornerRadius: 12))
                                    .padding(.horizontal, 24)
                            }
                        }
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .clipped()
                    .contentShape(Rectangle())
                    .onTapGesture(count: 2) { Task { await toggleFavorite(item) } }
                    .onTapGesture { if active { playback.toggle() } }
                    .overlay(alignment: .bottom) { caption(item, width: videoWidth) }
                    if active {
                        ReelSeekBar(progress: playback.progress) { playback.seek(to: $0) }
                            .padding(.horizontal, 10)
                            .padding(.bottom, 6)
                    }
                }
                .frame(width: videoWidth, height: geo.size.height)
            }
            .frame(width: geo.size.width, height: geo.size.height)
        }
    }

    private func caption(_ item: MediaItem, width: CGFloat) -> some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 6) {
                Text(item.title)
                    .font(Rs.display(22))
                    .lineLimit(2)
                let people = Array(item.performers).prefix(3).joined(separator: " · ")
                if !people.isEmpty {
                    Text(people)
                        .font(Rs.text(14))
                        .foregroundStyle(.white.opacity(0.78))
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: width * 0.72, alignment: .leading)
            Spacer(minLength: 8)
            VStack(spacing: 18) {
                Button { Task { await toggleFavorite(item) } } label: {
                    Image(systemName: item.favorite ? "heart.fill" : "heart")
                        .font(.title2)
                        .foregroundStyle(item.favorite ? Rs.accent : .white)
                }
                Button { playback.muted.toggle() } label: {
                    Image(systemName: playback.muted ? "speaker.slash.fill" : "speaker.wave.2.fill")
                        .font(.title3)
                }
                Button { open(.media(item.id)) } label: {
                    Image(systemName: "info.circle")
                        .font(.title2)
                }
            }
            .foregroundStyle(.white)
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 28)
        .frame(width: width)
        .background(
            LinearGradient(colors: [.clear, .black.opacity(0.72)], startPoint: .top, endPoint: .bottom)
                .frame(height: 180)
                .allowsHitTesting(false),
            alignment: .bottom
        )
    }

    private func toggleFavorite(_ item: MediaItem) async {
        guard let session = model.session else { return }
        guard let updated = try? await awaitCall({ session.setFavorite(id: item.id, favorite: !item.favorite, onOk: $0, onErr: $1) }),
              let index = items.firstIndex(where: { $0.id == item.id }) else { return }
        items[index] = updated
    }

    private func loadMore() async {
        guard let session = model.session, !loading else { return }
        if total > 0, items.count >= total { return }
        loading = true
        defer { loading = false }
        do {
            let page = try await awaitCall {
                session.reels(limit: 12, offset: Int32(items.count), seed: seed, onOk: $0, onErr: $1)
            }
            let seen = Set(items.map(\.id))
            items.append(contentsOf: Array(page.items).filter { !seen.contains($0.id) })
            total = Int(page.total)
            if page.items.isEmpty { total = items.count }
            if current == nil { current = items.first?.id }
            error = nil
        } catch {
            if items.isEmpty { self.error = error.localizedDescription }
        }
    }
}

struct ReelSeekBar: View {
    let progress: Double
    var onSeek: (Double) -> Void
    @State private var drag: Double?

    var body: some View {
        GeometryReader { geo in
            let value = drag ?? progress
            ZStack(alignment: .leading) {
                Capsule().fill(.white.opacity(0.35)).frame(height: drag == nil ? 3 : 6)
                Capsule().fill(.white).frame(width: max(6, geo.size.width * value), height: drag == nil ? 3 : 6)
            }
            .frame(maxHeight: .infinity, alignment: .center)
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { gesture in
                        let fraction = min(1, max(0, gesture.location.x / max(geo.size.width, 1)))
                        drag = fraction
                        onSeek(fraction)
                    }
                    .onEnded { gesture in
                        let fraction = min(1, max(0, gesture.location.x / max(geo.size.width, 1)))
                        onSeek(fraction)
                        drag = nil
                    }
            )
        }
        .frame(height: 28)
    }
}

@MainActor
final class ReelPlayback: ObservableObject {
    let player = AVPlayer()
    @Published var paused = false
    @Published var muted = false
    @Published var progress: Double = 0
    @Published var error: String?
    private var playingId: Int64 = -1
    private var observer: Any?
    private var endObserver: NSObjectProtocol?
    private var scrubbing = false

    func play(item: MediaItem, session: AppleSession?, headers: [String: String]) async {
        guard let session, playingId != item.id else { return }
        playingId = item.id
        error = nil
        progress = 0
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = nil
        player.replaceCurrentItem(with: nil)
        do {
            let plan = try await awaitCall { session.preparePlayback(mediaId: item.id, forceTranscode: false, onOk: $0, onErr: $1) }
            guard playingId == item.id, let url = URL(string: plan.url) else { return }
            let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
            let template = AVPlayerItem(asset: asset)
            player.replaceCurrentItem(with: template)
            endObserver = NotificationCenter.default.addObserver(forName: .AVPlayerItemDidPlayToEndTime, object: template, queue: .main) { [weak self] _ in
                Task { @MainActor in
                    self?.player.seek(to: .zero)
                    self?.player.play()
                }
            }
            player.isMuted = muted
            try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try? AVAudioSession.sharedInstance().setActive(true)
            observe()
            player.play()
            paused = false
        } catch {
            if playingId == item.id { self.error = error.localizedDescription }
        }
    }

    func seek(to fraction: Double) {
        let clamped = min(1, max(0, fraction))
        progress = clamped
        guard let duration = player.currentItem?.duration.seconds, duration.isFinite, duration > 0 else { return }
        scrubbing = true
        let time = CMTime(seconds: duration * clamped, preferredTimescale: 600)
        player.seek(to: time, toleranceBefore: .zero, toleranceAfter: .zero) { [weak self] _ in
            Task { @MainActor in self?.scrubbing = false }
        }
    }

    func toggle() {
        if player.rate == 0 {
            player.play()
            paused = false
        } else {
            player.pause()
            paused = true
        }
    }

    func stop() {
        if let observer { player.removeTimeObserver(observer) }
        observer = nil
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        endObserver = nil
        player.pause()
        player.replaceCurrentItem(with: nil)
        playingId = -1
    }

    private func observe() {
        if let observer { player.removeTimeObserver(observer) }
        observer = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.2, preferredTimescale: 600), queue: .main) { [weak self] time in
            guard let self else { return }
            Task { @MainActor in
                guard !self.scrubbing else { return }
                let duration = self.player.currentItem?.duration.seconds ?? 0
                self.progress = duration > 0 && time.seconds.isFinite ? min(1, time.seconds / duration) : 0
                self.player.isMuted = self.muted
            }
        }
    }
}

struct ReelPlayerView: UIViewRepresentable {
    let player: AVPlayer

    func makeUIView(context: Context) -> ReelPlayerUIView {
        let view = ReelPlayerUIView()
        view.playerLayer.player = player
        view.playerLayer.videoGravity = .resizeAspectFill
        view.backgroundColor = .black
        return view
    }

    func updateUIView(_ view: ReelPlayerUIView, context: Context) {
        view.playerLayer.player = player
    }
}

final class ReelPlayerUIView: UIView {
    override class var layerClass: AnyClass { AVPlayerLayer.self }
    var playerLayer: AVPlayerLayer { layer as! AVPlayerLayer }
}

struct LibraryView: View {
    @EnvironmentObject private var model: AppModel
    var open: (MediaRoute) -> Void
    @State private var favorites: [MediaItem] = []
    @State private var later: [MediaItem] = []
    @State private var playlists: [Collection] = []
    @State private var error: String?

    var body: some View {
        List {
            if let error { Text(error).foregroundStyle(Rs.accent) }
            Section("Favorites") {
                if favorites.isEmpty { Text("Nothing saved yet").foregroundStyle(Rs.tertiary) }
                ForEach(favorites, id: \.id) { item in
                    Button(item.title) { open(.media(item.id)) }
                }
            }
            Section("Watch later") {
                if later.isEmpty { Text("Your list is empty").foregroundStyle(Rs.tertiary) }
                ForEach(later, id: \.id) { item in
                    Button(item.title) { open(.media(item.id)) }
                }
            }
            Section("Playlists") {
                if playlists.isEmpty { Text("No playlists").foregroundStyle(Rs.tertiary) }
                ForEach(playlists, id: \.id) { list in
                    Button {
                        open(.playlist(list.id))
                    } label: {
                        HStack {
                            Text(list.name)
                            Spacer()
                            Text("\(list.mediaCount)").foregroundStyle(Rs.tertiary)
                        }
                    }
                }
            }
        }
        .navigationTitle("Library")
        .toolbarTitleDisplayMode(.inlineLarge)
        .refreshable { await load() }
        .task { await load() }
    }

    private func load() async {
        guard let session = model.session else { return }
        do {
            favorites = try await awaitCall {
                session.media(kind: "", sort: "", favorite: true, watchLater: false, reels: false, text: "", seriesId: -1, performerId: -1, limit: 40, offset: 0, onOk: $0, onErr: $1)
            }.items
            later = try await awaitCall {
                session.media(kind: "", sort: "", favorite: false, watchLater: true, reels: false, text: "", seriesId: -1, performerId: -1, limit: 40, offset: 0, onOk: $0, onErr: $1)
            }.items
            playlists = try await awaitCall { session.playlists(onOk: $0, onErr: $1) }.items
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct SearchView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    var open: (MediaRoute) -> Void
    @State private var text = ""
    @State private var hits: [SearchHit] = []
    @State private var error: String?

    var body: some View {
        List {
            if let error { Text(error).foregroundStyle(Rs.accent) }
            ForEach(hits, id: \.id) { hit in
                Button {
                    dismiss()
                    switch hit.type {
                    case "performer": open(.person(hit.id))
                    case "series": open(.series(hit.id))
                    default: open(.media(hit.id))
                    }
                } label: {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(hit.label)
                        Text(hit.type.replacingOccurrences(of: "_", with: " ")).font(.caption).foregroundStyle(Rs.tertiary)
                    }
                }
            }
        }
        .scrollContentBackground(.hidden)
        .navigationTitle("Search")
        .searchable(text: $text, prompt: "Titles, people, shows")
        .onSubmit(of: .search) { Task { await run() } }
        .onChange(of: text) { _, _ in Task { await run() } }
    }

    private func run() async {
        let query = text.trimmingCharacters(in: .whitespaces)
        guard query.count >= 2, let session = model.session else {
            hits = []
            return
        }
        do {
            hits = try await awaitCall { session.search(text: query, onOk: $0, onErr: $1) }.items
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct MediaDetailView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    let id: Int64
    var play: (PlayRequest) -> Void
    var open: (MediaRoute) -> Void

    @State private var item: MediaItem?
    @State private var related: [MediaItem] = []
    @State private var studioMore: [MediaItem] = []
    @State private var seriesParts: [MediaItem] = []
    @State private var error: String?

    var body: some View {
        ScrollView {
            if let item {
                VStack(alignment: .leading, spacing: size == .regular ? 28 : 20) {
                    backdrop(item)
                    titleBlock(item)
                    if !seriesParts.isEmpty { seriesShelf(item) }
                    facts(item)
                    shelf("Suggested", "Similar titles", items: related.filter { $0.id != item.id }, tone: 5)
                    shelf(item.studioName ?? "Studio", "More from this studio", items: studioMore.filter { $0.id != item.id }, tone: 1)
                }
                .padding(.bottom, 32)
            } else if let error {
                Text(error).foregroundStyle(Rs.accentText).padding()
            } else {
                ProgressView().padding(.top, 80).frame(maxWidth: .infinity)
            }
        }
        .contentMargins(.top, 0, for: .scrollContent)
        .ignoresSafeArea(edges: .top)
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .task(id: id) { await load() }
    }

    private var gutter: CGFloat { size == .regular ? 32 : 16 }
    private var posterWidth: CGFloat { size == .regular ? 168 : 112 }
    private var posterHeight: CGFloat { posterWidth * 1.5 }
    private var backdropHeight: CGFloat { size == .regular ? 460 : 300 }

    private func backdrop(_ item: MediaItem) -> some View {
        AuthImage(
            url: backdropURL(item, session: model.session) ?? posterURL(item, session: model.session),
            fallback: thumbnailURL(item, session: model.session),
            headers: model.headers(),
            initial: item.title
        )
        .frame(maxWidth: .infinity)
        .frame(height: backdropHeight)
        .clipped()
        .overlay(alignment: .bottom) {
            LinearGradient(
                stops: [
                    .init(color: .clear, location: 0.4),
                    .init(color: Rs.canvas.opacity(0.72), location: 0.78),
                    .init(color: Rs.canvas, location: 1),
                ],
                startPoint: .top,
                endPoint: .bottom
            )
            .frame(height: backdropHeight * 0.55)
        }
    }

    private func titleBlock(_ item: MediaItem) -> some View {
        HStack(alignment: .top, spacing: size == .regular ? 24 : 14) {
            AuthImage(
                url: posterURL(item, session: model.session),
                fallback: thumbnailURL(item, session: model.session),
                headers: model.headers(),
                initial: item.title
            )
                .frame(width: posterWidth, height: posterHeight)
                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(.white.opacity(0.12), lineWidth: 1))
            VStack(alignment: .leading, spacing: 12) {
                if let studio = item.studioName, !studio.isEmpty {
                    Text(studio)
                        .font(Rs.text(12, weight: .bold))
                        .tracking(1.4)
                        .textCase(.uppercase)
                        .foregroundStyle(Rs.accentText)
                }
                Text(item.title)
                    .font(Rs.display(size == .regular ? 40 : 26))
                    .foregroundStyle(Rs.text)
                    .fixedSize(horizontal: false, vertical: true)
                Text(metaLine(item))
                    .font(Rs.text(14, weight: .medium))
                    .foregroundStyle(Rs.secondary)
                if item.appleProgress() >= 0 {
                    ProgressView(value: Double(item.appleProgress()))
                        .tint(Rs.accent)
                }
                actions(item)
                let overview = item.appleOverview()
                if !overview.isEmpty {
                    Text(overview)
                        .font(Rs.text(size == .regular ? 17 : 16))
                        .foregroundStyle(Rs.text)
                        .lineSpacing(4)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.horizontal, gutter)
    }

    private func metaLine(_ item: MediaItem) -> String {
        var parts: [String] = []
        let meta = item.appleMeta()
        if !meta.isEmpty { parts.append(meta) }
        if let series = item.seriesName, !series.isEmpty { parts.append(series) }
        let minutes = item.appleMinutes()
        if minutes > 0 { parts.append("\(minutes) min") }
        return parts.joined(separator: "  ·  ")
    }

    private func facts(_ item: MediaItem) -> some View {
        let cast = Array(item.cast)
        let names = cast.isEmpty ? Array(item.performers) : []
        let directors = Array(item.crew).filter { $0.job == "director" }
        let writers = Array(item.crew).filter { $0.job == "writer" }
        let chips = Array(item.categories) + Array(item.tags)
        return VStack(alignment: .leading, spacing: 18) {
            if !cast.isEmpty || !names.isEmpty {
                faceRow(model.entertainment ? "Cast" : "Performers", people: cast, names: names)
            }
            if !directors.isEmpty { faceRow(directors.count == 1 ? "Director" : "Directors", people: directors, names: []) }
            if !writers.isEmpty { faceRow(writers.count == 1 ? "Writer" : "Writers", people: writers, names: []) }
            VStack(alignment: .leading, spacing: 0) {
                factRow("Studio", item.studioName ?? "")
                if item.appleSeriesId() >= 0 {
                    Button { open(.series(item.appleSeriesId())) } label: {
                        factRow(item.kind == "episode" ? "Show" : "Series", item.seriesName ?? "")
                    }
                    .buttonStyle(.plain)
                }
                factRow("Released", String(item.releaseDate?.prefix(10) ?? ""))
            }
            .background(Rs.surface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            if !chips.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        ForEach(chips, id: \.self) { chip in
                            Text(chip)
                                .font(Rs.text(13, weight: .medium))
                                .padding(.horizontal, 12)
                                .padding(.vertical, 7)
                                .background(Rs.surface, in: Capsule())
                        }
                    }
                }
            }
        }
        .padding(.horizontal, gutter)
    }

    private func faceRow(_ title: String, people: [Person], names: [String]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .font(Rs.text(13, weight: .bold))
                .foregroundStyle(Rs.secondary)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 14) {
                    ForEach(people, id: \.id) { person in
                        Button { open(.person(person.id)) } label: { face(person.name, person.appleCharacter(), person.appleImageId(), person.id) }
                            .buttonStyle(.plain)
                    }
                    ForEach(names, id: \.self) { name in
                        face(name, "", -1, 0)
                    }
                }
            }
        }
    }

    private func face(_ name: String, _ character: String, _ imageId: Int64, _ personId: Int64) -> some View {
        VStack(spacing: 6) {
            AuthImage(
                url: imageId >= 0 ? URL(string: model.session?.performerUrl(id: personId) ?? "") : nil,
                headers: model.headers(),
                initial: name
            )
            .frame(width: size == .regular ? 84 : 68, height: size == .regular ? 84 : 68)
            .clipShape(Circle())
            Text(name)
                .font(Rs.text(12, weight: .semibold))
                .foregroundStyle(Rs.text)
                .lineLimit(2)
                .multilineTextAlignment(.center)
            if !character.isEmpty {
                Text(character)
                    .font(Rs.text(11))
                    .foregroundStyle(Rs.tertiary)
                    .lineLimit(2)
                    .multilineTextAlignment(.center)
            }
        }
        .frame(width: size == .regular ? 96 : 78)
    }

    private func factRow(_ label: String, _ value: String) -> some View {
        Group {
            if !value.isEmpty {
                HStack {
                    Text(label).foregroundStyle(Rs.tertiary)
                    Spacer()
                    Text(value).foregroundStyle(Rs.text).multilineTextAlignment(.trailing)
                }
                .font(Rs.text(15))
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
            }
        }
    }

    private func seriesShelf(_ item: MediaItem) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            ShelfHeader(eyebrow: "Series", title: "More in \(item.seriesName ?? "this series")", tone: Rs.tone(3))
                .padding(.horizontal, gutter)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 14) {
                    ForEach(seriesParts, id: \.id) { part in
                        PosterCard(item: part, shape: .landscape, width: size == .regular ? 280 : 220) { open(.media(part.id)) }
                            .opacity(part.id == item.id ? 0.55 : 1)
                    }
                }
                .padding(.horizontal, gutter)
            }
        }
    }

    private func shelf(_ eyebrow: String, _ title: String, items: [MediaItem], tone: Int) -> some View {
        Group {
            if !items.isEmpty {
                VStack(alignment: .leading, spacing: 12) {
                    ShelfHeader(eyebrow: eyebrow, title: title, tone: Rs.tone(tone))
                        .padding(.horizontal, gutter)
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 14) {
                            ForEach(items, id: \.id) { other in
                                PosterCard(item: other, width: size == .regular ? 150 : 120) { open(.media(other.id)) }
                            }
                        }
                        .padding(.horizontal, gutter)
                    }
                }
            }
        }
    }

    private func actions(_ item: MediaItem) -> some View {
        GlassEffectContainer(spacing: 10) {
            HStack(spacing: 10) {
                Button {
                    play(PlayRequest(id: item.id, title: item.title, resume: resume(item)))
                } label: {
                    Label(item.appleProgress() >= 0 ? "Resume" : "Play", systemImage: "play.fill")
                        .padding(.horizontal, 8)
                }
                .rsProminent()
                toggle("heart", on: item.favorite, label: "Favorite") { await setFavorite(!item.favorite) }
                toggle("bookmark", on: item.watchLater, label: "Later") { await setLater(!item.watchLater) }
                toggle("checkmark", on: item.watched, label: "Watched") { await setWatched(!item.watched) }
            }
        }
    }

    private func toggle(_ symbol: String, on: Bool, label: String, action: @escaping () async -> Void) -> some View {
        Button { Task { await action() } } label: {
            Image(systemName: on ? "\(symbol).fill" : symbol)
                .frame(width: 22, height: 22)
                .padding(8)
        }
        .buttonStyle(.glass)
        .tint(on ? Rs.accent : Rs.text)
        .accessibilityLabel(label)
    }

    private func load() async {
        guard let session = model.session else { return }
        do {
            let loaded = try await awaitCall { session.mediaItem(id: id, onOk: $0, onErr: $1) }
            item = loaded
            related = (try? await awaitCall { session.related(id: id, onOk: $0, onErr: $1) })?.items ?? []
            if loaded.appleStudioId() >= 0 {
                studioMore = (try? await awaitCall { session.studioTitles(studioId: loaded.appleStudioId(), onOk: $0, onErr: $1) })?.items ?? []
            }
            if loaded.appleSeriesId() >= 0 {
                seriesParts = (try? await awaitCall {
                    session.media(kind: "", sort: "series", favorite: false, watchLater: false, reels: false, text: "", seriesId: loaded.appleSeriesId(), performerId: -1, limit: 40, offset: 0, onOk: $0, onErr: $1)
                })?.items ?? []
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func setFavorite(_ on: Bool) async {
        guard let session = model.session else { return }
        item = try? await awaitCall { session.setFavorite(id: id, favorite: on, onOk: $0, onErr: $1) }
    }

    private func setWatched(_ on: Bool) async {
        guard let session = model.session else { return }
        item = try? await awaitCall { session.setWatched(id: id, watched: on, onOk: $0, onErr: $1) }
    }

    private func setLater(_ on: Bool) async {
        guard let session = model.session else { return }
        do {
            try await awaitDone { session.setWatchLater(id: id, on: on, onOk: $0, onErr: $1) }
            await load()
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct SeriesDetailView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    let id: Int64
    var open: (MediaRoute) -> Void
    @State private var series: Series?
    @State private var episodes: [MediaItem] = []
    @State private var error: String?

    private var gutter: CGFloat { size == .regular ? 36 : 20 }
    private var watchedCount: Int { episodes.filter(\.watched).count }

    var body: some View {
        ScrollView {
            if let series {
                VStack(alignment: .leading, spacing: size == .regular ? 28 : 20) {
                    banner(series)
                    if !episodes.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            ShelfHeader(eyebrow: "In order", title: episodes.count == 1 ? "1 title" : "\(episodes.count) titles", tone: Rs.tone(2))
                            if watchedCount > 0 {
                                HStack(spacing: 10) {
                                    ProgressView(value: Double(watchedCount), total: Double(episodes.count))
                                        .tint(Rs.accent)
                                    Text("\(watchedCount) watched")
                                        .font(Rs.text(12, weight: .medium))
                                        .foregroundStyle(Rs.secondary)
                                        .fixedSize()
                                }
                            }
                            VStack(spacing: 10) {
                                ForEach(Array(episodes.enumerated()), id: \.element.id) { index, item in
                                    EpisodeRow(index: index + 1, item: item) { open(.media(item.id)) }
                                }
                            }
                        }
                        .padding(.horizontal, gutter)
                    } else if error == nil {
                        CatalogMessage(symbol: "film", title: "No titles in this series", message: "Episodes and parts will line up here once they’re linked.")
                    }
                }
                .padding(.bottom, 36)
            } else if let error {
                CatalogMessage(symbol: "exclamationmark.triangle", title: "This series didn’t load", message: error)
            } else {
                ProgressView().controlSize(.large).tint(Rs.accent).frame(maxWidth: .infinity).padding(.top, 120)
            }
        }
        .contentMargins(.top, 0, for: .scrollContent)
        .ignoresSafeArea(edges: .top)
        .navigationTitle(series?.name ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbarColorScheme(.dark, for: .navigationBar)
        .task(id: id) { await load() }
    }

    private func banner(_ series: Series) -> some View {
        let height: CGFloat = size == .regular ? 460 : 320
        let poster: CGFloat = size == .regular ? 168 : 112
        return ZStack(alignment: .bottomLeading) {
            AuthImage(
                url: seriesBackdropURL(series, session: model.session) ?? seriesPosterURL(series, session: model.session),
                fallback: seriesPosterURL(series, session: model.session),
                headers: model.headers(),
                initial: series.name
            )
            .frame(height: height)
            .frame(maxWidth: .infinity)
            LinearGradient(stops: [
                .init(color: .clear, location: 0.2),
                .init(color: Rs.canvas.opacity(0.35), location: 0.55),
                .init(color: Rs.canvas, location: 1),
            ], startPoint: .top, endPoint: .bottom)
            HStack(alignment: .bottom, spacing: size == .regular ? 24 : 14) {
                AuthImage(
                    url: seriesPosterURL(series, session: model.session),
                    fallback: seriesBackdropURL(series, session: model.session),
                    headers: model.headers(),
                    initial: series.name
                )
                .frame(width: poster, height: poster * 1.5)
                .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 16, style: .continuous)
                        .strokeBorder(.white.opacity(0.18), lineWidth: 1)
                }
                .shadow(color: .black.opacity(0.45), radius: 18, y: 10)
                VStack(alignment: .leading, spacing: 8) {
                    Text("SERIES")
                        .font(Rs.text(12, weight: .bold))
                        .tracking(1.5)
                        .foregroundStyle(Rs.accentText)
                    Text(series.name)
                        .font(Rs.display(size == .regular ? 48 : 32))
                        .lineLimit(3)
                    Text(seriesBannerMeta(series))
                        .font(Rs.text(14, weight: .medium))
                        .foregroundStyle(Rs.secondary)
                    let about = (series.description_ ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                    if !about.isEmpty {
                        Text(about)
                            .font(Rs.text(16))
                            .foregroundStyle(Rs.text)
                            .lineLimit(size == .regular ? 4 : 3)
                    }
                }
                .padding(.bottom, 4)
            }
            .padding(.horizontal, gutter)
            .padding(.bottom, 8)
        }
    }

    private func seriesBannerMeta(_ series: Series) -> String {
        var parts = [series.mediaCount == 1 ? "1 title" : "\(series.mediaCount) titles"]
        if series.seasonCount > 0 {
            parts.append(series.seasonCount == 1 ? "1 season" : "\(series.seasonCount) seasons")
        }
        if let studio = series.studioName, !studio.isEmpty { parts.append(studio) }
        return parts.joined(separator: " · ")
    }

    private func load() async {
        guard let session = model.session else { return }
        do {
            series = try await awaitCall { session.seriesDetail(id: id, onOk: $0, onErr: $1) }
            let page = try await awaitCall {
                session.media(kind: "", sort: "series", favorite: false, watchLater: false, reels: false, text: "", seriesId: id, performerId: -1, limit: 200, offset: 0, onOk: $0, onErr: $1)
            }
            episodes = Array(page.items)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct EpisodeRow: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    let index: Int
    let item: MediaItem
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: size == .regular ? 16 : 12) {
                Text(String(format: "%02d", index))
                    .font(Rs.display(18))
                    .foregroundStyle(Rs.tertiary)
                    .frame(width: size == .regular ? 36 : 28, alignment: .center)
                let thumb: CGFloat = size == .regular ? 168 : 112
                Color.clear
                    .frame(width: thumb, height: thumb * 9 / 16)
                    .overlay {
                        AuthImage(
                            url: backdropURL(item, session: model.session) ?? posterURL(item, session: model.session),
                            fallback: thumbnailURL(item, session: model.session),
                            headers: model.headers(),
                            initial: item.title
                        )
                        .opacity(item.watched ? 0.55 : 1)
                    }
                    .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                VStack(alignment: .leading, spacing: 4) {
                    Text(item.title)
                        .font(Rs.text(size == .regular ? 17 : 15, weight: .semibold))
                        .lineLimit(2)
                        .multilineTextAlignment(.leading)
                    let meta = episodeMeta
                    if !meta.isEmpty {
                        Text(meta)
                            .font(Rs.text(12))
                            .foregroundStyle(Rs.secondary)
                            .lineLimit(1)
                    }
                    if item.appleProgress() >= 0 {
                        ProgressView(value: Double(item.appleProgress()))
                            .tint(Rs.accent)
                    }
                }
                Spacer(minLength: 0)
                if item.watched {
                    Image(systemName: "checkmark.circle.fill")
                        .foregroundStyle(Rs.accent)
                }
            }
            .padding(size == .regular ? 10 : 8)
            .background(Rs.surface, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        }
        .buttonStyle(RsPressStyle())
    }

    private var episodeMeta: String {
        var parts: [String] = []
        let label = item.appleMeta()
        if !label.isEmpty { parts.append(label) }
        let minutes = item.appleMinutes()
        if minutes > 0 { parts.append("\(minutes) min") }
        let names = Array(item.performers).prefix(2)
        if !names.isEmpty { parts.append(names.joined(separator: ", ")) }
        return parts.joined(separator: " · ")
    }
}

struct PersonDetailView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    let id: Int64
    var open: (MediaRoute) -> Void
    @State private var person: Performer?
    @State private var titles: [MediaItem] = []

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if let person {
                    HStack(spacing: 16) {
                        AuthImage(url: person.appleImageId() >= 0 ? URL(string: model.session?.performerUrl(id: person.id) ?? "") : nil, headers: model.headers(), initial: person.name)
                            .frame(width: 96, height: 96)
                            .clipShape(Circle())
                        Text(person.name).font(.title.weight(.semibold))
                    }
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 150 : 110), spacing: 14)], spacing: 14) {
                    ForEach(titles, id: \.id) { item in
                        PosterCard(item: item) { open(.media(item.id)) }
                    }
                }
            }
            .padding(size == .regular ? 28 : 16)
        }
        .navigationTitle(person?.name ?? "Person")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let session = model.session else { return }
        person = try? await awaitCall { session.performer(id: id, onOk: $0, onErr: $1) }
        let page = try? await awaitCall {
            session.media(kind: "", sort: "", favorite: false, watchLater: false, reels: false, text: "", seriesId: -1, performerId: id, limit: 80, offset: 0, onOk: $0, onErr: $1)
        }
        titles = page?.items ?? []
    }
}

struct PlaylistDetailView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    let id: Int64
    var open: (MediaRoute) -> Void
    @State private var detail: CollectionDetail?
    @State private var error: String?

    private var gutter: CGFloat { size == .regular ? 28 : 16 }
    private var items: [MediaItem] { Array(detail?.items ?? []) }

    var body: some View {
        ScrollView {
            if let detail {
                VStack(alignment: .leading, spacing: 22) {
                    header(detail.collection)
                    if items.isEmpty {
                        CatalogMessage(symbol: "film", title: "This playlist is empty", message: detail.collection.smart ? "It fills itself when titles match its rules." : "Add titles from the web and they’ll appear here.")
                    } else {
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: size == .regular ? 160 : 110), spacing: 16)], spacing: 16) {
                            ForEach(items, id: \.id) { item in
                                PosterCard(item: item) { open(.media(item.id)) }
                            }
                        }
                    }
                }
                .padding(.horizontal, gutter)
                .padding(.bottom, 32)
            } else if let error {
                CatalogMessage(symbol: "exclamationmark.triangle", title: "This playlist didn’t load", message: error)
            } else {
                ProgressView().controlSize(.large).tint(Rs.accent).frame(maxWidth: .infinity).padding(.top, 120)
            }
        }
        .navigationTitle(detail?.collection.name ?? "Playlist")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: id) { await load() }
    }

    private func header(_ list: Collection) -> some View {
        HStack(alignment: .bottom, spacing: 18) {
            PlaylistMosaic(covers: Array(list.covers).prefix(4).map(\.id), name: list.name)
                .frame(width: size == .regular ? 220 : 132)
                .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 18, style: .continuous)
                        .strokeBorder(.white.opacity(0.14), lineWidth: 1)
                }
            VStack(alignment: .leading, spacing: 8) {
                Text(list.smart ? "SMART PLAYLIST" : "PLAYLIST")
                    .font(Rs.text(12, weight: .bold))
                    .tracking(1.5)
                    .foregroundStyle(Rs.accentText)
                Text(list.name)
                    .font(Rs.display(size == .regular ? 40 : 28))
                    .lineLimit(3)
                HStack(spacing: 8) {
                    if list.pinned {
                        Label("Pinned", systemImage: "pin.fill")
                    }
                    if list.smart {
                        Label("Smart", systemImage: "sparkles")
                    }
                    Text(list.mediaCount == 1 ? "1 title" : "\(list.mediaCount) titles")
                }
                .font(Rs.text(14, weight: .medium))
                .foregroundStyle(Rs.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(.top, 12)
    }

    private func load() async {
        guard let session = model.session else { return }
        do {
            detail = try await awaitCall { session.playlist(id: id, onOk: $0, onErr: $1) }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

enum ShelfShape { case poster, landscape }

struct PosterCard: View {
    @EnvironmentObject private var model: AppModel
    let item: MediaItem
    var shape: ShelfShape = .poster
    var width: CGFloat?
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(alignment: .leading, spacing: 8) {
                ZStack(alignment: .bottomLeading) {
                    AuthImage(
                        url: shape == .landscape
                            ? (backdropURL(item, session: model.session) ?? posterURL(item, session: model.session))
                            : posterURL(item, session: model.session),
                        fallback: thumbnailURL(item, session: model.session),
                        headers: model.headers(),
                        initial: item.title
                    )
                    .aspectRatio(shape == .landscape ? 16 / 9 : 2 / 3, contentMode: .fit)
                    LinearGradient(colors: [.clear, .black.opacity(shape == .landscape ? 0.72 : 0)], startPoint: .center, endPoint: .bottom)
                    if shape == .landscape {
                        Text(item.title)
                            .font(Rs.text(14, weight: .semibold))
                            .lineLimit(2)
                            .padding(10)
                    }
                    let progress = item.appleProgress()
                    if progress >= 0 {
                        ProgressView(value: Double(progress))
                            .tint(Rs.accent)
                            .padding(.horizontal, 8)
                            .padding(.bottom, 6)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: shape == .landscape ? 18 : 14, style: .continuous))
                .shadow(color: .black.opacity(0.35), radius: 10, y: 6)
                if shape == .poster {
                    Text(item.title).font(Rs.text(13, weight: .medium)).lineLimit(2)
                    let meta = item.appleMeta()
                    if !meta.isEmpty {
                        Text(meta).font(Rs.text(11)).foregroundStyle(Rs.tertiary)
                    }
                }
            }
            .frame(width: width, alignment: .leading)
        }
        .buttonStyle(.plain)
    }
}

struct CatalogMessage: View {
    let symbol: String
    let title: String
    let message: String

    var body: some View {
        VStack(spacing: 10) {
            Image(systemName: symbol)
                .font(.system(size: 28, weight: .light))
                .foregroundStyle(Rs.accentText)
                .frame(width: 56, height: 56)
                .rsGlass(in: Circle(), interactive: false)
            Text(title)
                .font(Rs.display(26))
                .multilineTextAlignment(.center)
            Text(message)
                .font(Rs.text(15))
                .foregroundStyle(Rs.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 360)
        }
        .frame(maxWidth: .infinity)
        .padding(.top, 72)
        .padding(.horizontal, 24)
    }
}

struct RsPressStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.spring(duration: 0.28, bounce: 0.15), value: configuration.isPressed)
    }
}

func catalogSkeleton(ratio: CGFloat, minimum: CGFloat) -> some View {
    LazyVGrid(columns: [GridItem(.adaptive(minimum: minimum), spacing: 16)], spacing: 16) {
        ForEach(0..<6, id: \.self) { _ in
            RoundedRectangle(cornerRadius: 22, style: .continuous)
                .fill(Rs.surface)
                .aspectRatio(ratio, contentMode: .fit)
                .redacted(reason: .placeholder)
        }
    }
}

func seriesPosterURL(_ series: Series, session: AppleSession?) -> URL? {
    guard series.applePosterId() >= 0, let session else { return nil }
    return URL(string: session.seriesUrl(id: series.id))
}

func seriesBackdropURL(_ series: Series, session: AppleSession?) -> URL? {
    guard series.appleBackdropId() >= 0, let session else { return nil }
    return URL(string: session.seriesBackdropUrl(id: series.id))
}

func backdropURL(_ item: MediaItem, session: AppleSession?) -> URL? {
    let id = item.appleBackdropId()
    guard id >= 0, let session else { return nil }
    return URL(string: session.backdropUrl(mediaId: item.id))
}

func posterURL(_ item: MediaItem, session: AppleSession?) -> URL? {
    guard let session else { return nil }
    if item.applePosterId() >= 0 {
        return URL(string: session.posterUrl(mediaId: item.id))
    }
    return URL(string: session.thumbnailUrl(mediaId: item.id))
}

func thumbnailURL(_ item: MediaItem, session: AppleSession?) -> URL? {
    guard let session else { return nil }
    return URL(string: session.thumbnailUrl(mediaId: item.id))
}

func resume(_ item: MediaItem) -> Double {
    let progress = WatchProgress.companion
    let position = KotlinDouble(double: item.applePosition())
    let duration = item.appleDuration() > 0 ? KotlinDouble(double: item.appleDuration()) : nil
    return progress.resumeFrom(positionSeconds: position, durationSeconds: duration)
}
