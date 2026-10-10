import SwiftUI

@main
struct ReelStashApp: App {
    @StateObject private var model = AppModel()
    @Environment(\.scenePhase) private var scene

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(model)
                .preferredColorScheme(.dark)
                .tint(model.palette.accent)
                .onAppear { model.start() }
                .onChange(of: scene) { phase in
                    if phase == .active { Task { await model.refreshColorTheme() } }
                }
        }
    }
}

struct RootView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size

    var body: some View {
        Group {
            switch model.gate {
            case .starting:
                ProgressView().rsScreen()
            case .connect:
                ConnectView().rsScreen()
            case .signIn(let url, let info):
                SignInView(url: url, info: info).rsScreen()
            case .locked:
                LockView().rsScreen()
            case .ready:
                LibraryShell()
            }
        }
        .animation(.easeInOut(duration: 0.2), value: gateKey)
    }

    private var gateKey: String {
        switch model.gate {
        case .starting: "starting"
        case .connect: "connect"
        case .signIn: "signIn"
        case .locked: "locked"
        case .ready: "ready"
        }
    }
}

/// Fixed sidebar destinations. Labels do not change with the library mode.
enum SideLink: String, Hashable, CaseIterable {
    case home, titles, shows, series, reels, favorites, later, playlists, people, settings

    /// The Apple TV-style bar shown when the sidebar is collapsed.
    static let topBar: [SideLink] = [.home, .titles, .reels, .series]

    var title: String {
        switch self {
        case .home: "Home"
        case .titles: "Library"
        case .shows: "TV shows"
        case .series: "Series"
        case .reels: "Reels"
        case .favorites: "Favorites"
        case .later: "Watch later"
        case .playlists: "Playlists"
        case .people: "Performers"
        case .settings: "Settings"
        }
    }

    var symbol: String {
        switch self {
        case .home: "house"
        case .titles: "film"
        case .shows: "tv"
        case .series: "rectangle.stack"
        case .reels: "play.rectangle.on.rectangle"
        case .favorites: "heart"
        case .later: "bookmark"
        case .playlists: "list.bullet"
        case .people: "person.2"
        case .settings: "gearshape"
        }
    }
}

struct LibraryShell: View {
    @EnvironmentObject private var model: AppModel
    @State private var link: SideLink? = .home
    @State private var columns = NavigationSplitViewVisibility.automatic
    @State private var path = NavigationPath()
    @State private var search = false
    @State private var playing: PlayRequest?

    private var sidebarCollapsed: Bool { columns == .detailOnly }

    var body: some View {
        NavigationSplitView(columnVisibility: $columns) {
            List(selection: $link) {
                Section {
                    ForEach([SideLink.home, .titles, .reels, .series], id: \.self) { item in
                        Label(item.title, systemImage: item.symbol).tag(item)
                    }
                }
                Section {
                    ForEach([SideLink.favorites, .later, .playlists, .people], id: \.self) { item in
                        Label(item.title, systemImage: item.symbol).tag(item)
                    }
                }
                Section {
                    Label("Settings", systemImage: "gearshape").tag(SideLink.settings)
                }
            }
            .navigationTitle("ReelStash")
            .safeAreaInset(edge: .bottom, spacing: 8) { librarySwitcher }
        } detail: {
            NavigationStack(path: $path) {
                detail
                    .id(model.libraryId)
                    .navigationDestination(for: MediaRoute.self) { route in
                        switch route {
                        case .media(let id):
                            MediaDetailView(id: id, play: { playing = $0 }, open: { path.append($0) })
                        case .series(let id):
                            SeriesDetailView(id: id, open: { path.append($0) })
                        case .person(let id):
                            PersonDetailView(id: id, open: { path.append($0) })
                        case .playlist(let id):
                            PlaylistDetailView(id: id, open: { path.append($0) })
                        }
                    }
                    .toolbar {
                        if link != .home {
                            if sidebarCollapsed {
                                ToolbarItem(placement: .principal) { topBar }
                            }
                            ToolbarItem(placement: .topBarTrailing) {
                                Button { search = true } label: { Image(systemName: "magnifyingglass") }
                            }
                        }
                    }
            }
            .rsScreen()
        }
        .navigationSplitViewStyle(.balanced)
        .tint(model.palette.accent)
        .onChange(of: link) { path = NavigationPath() }
        .onChange(of: model.libraryId) { path = NavigationPath() }
        .sheet(isPresented: $search) {
            NavigationStack { SearchView(open: { route in
                search = false
                path.append(route)
            }) }
            .environmentObject(model)
            .presentationBackground(Rs.canvas)
        }
        .fullScreenCover(item: $playing) { request in
            PlayerScreen(request: request).environmentObject(model)
        }
    }

    @ViewBuilder
    private var detail: some View {
        switch link ?? .home {
        case .home:
            HomeView(sidebarCollapsed: sidebarCollapsed, openSearch: { search = true }, open: { path.append($0) }, play: { playing = $0 }, select: { link = $0 })
        case .titles:
            MediaGridView(title: "Library", open: { path.append($0) })
        case .shows:
            SeriesGridView(title: "Series", shows: false, open: { path.append($0) })
        case .series:
            SeriesGridView(title: "Series", shows: false, open: { path.append($0) })
        case .reels:
            ReelsView(play: { playing = $0 }, open: { path.append($0) })
        case .favorites:
            MediaGridView(title: "Favorites", favorite: true, open: { path.append($0) })
        case .later:
            MediaGridView(title: "Watch later", watchLater: true, open: { path.append($0) })
        case .playlists:
            PlaylistListView(open: { path.append($0) })
        case .people:
            PeopleGridView(title: "Performers", open: { path.append($0) })
        case .settings:
            SettingsView()
        }
    }

    private var topBar: some View {
        HStack(spacing: 2) {
            ForEach(SideLink.topBar, id: \.self) { item in
                Button {
                    link = item
                } label: {
                    Text(item.title)
                        .font(Rs.text(15, weight: link == item ? .semibold : .regular))
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                        .background(link == item ? AnyShapeStyle(.white.opacity(0.16)) : AnyShapeStyle(.clear), in: Capsule())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(4)
        .rsGlass(in: Capsule())
    }

    private var librarySwitcher: some View {
        Menu {
            Button("All libraries") { model.selectLibrary(-1) }
            ForEach(model.libraries, id: \.id) { library in
                Button(library.name) { model.selectLibrary(library.id) }
            }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: model.library?.isEntertainment == true ? "film" : "lock")
                    .foregroundStyle(Rs.accentText)
                VStack(alignment: .leading, spacing: 1) {
                    Text(model.library?.name ?? "All libraries")
                        .font(Rs.text(15, weight: .semibold))
                        .lineLimit(1)
                    Text(model.library == nil ? model.serverName : (model.entertainment ? "Entertainment" : "Private"))
                        .font(Rs.text(12))
                        .foregroundStyle(Rs.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(Rs.tertiary)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .rsGlass(in: .rect(cornerRadius: 16))
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 12)
        .padding(.bottom, 10)
    }
}

enum MediaRoute: Hashable {
    case media(Int64)
    case series(Int64)
    case person(Int64)
    case playlist(Int64)
}

struct PlayRequest: Identifiable {
    let id: Int64
    let title: String
    let resume: Double
}
