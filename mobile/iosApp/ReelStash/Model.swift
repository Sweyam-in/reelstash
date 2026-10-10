import Foundation
import Shared
import SwiftUI
import UIKit

enum DeviceLabel {
    static var name: String { UIDevice.current.userInterfaceIdiom == .pad ? "iPad" : "iPhone" }
    static var platform: String { UIDevice.current.userInterfaceIdiom == .pad ? "ipados" : "ios" }
}

struct Failure: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

func awaitDone(_ start: (@escaping () -> Void, @escaping (String) -> Void) -> Void) async throws {
    try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
        start(
            { continuation.resume() },
            { continuation.resume(throwing: Failure(message: $0)) }
        )
    }
}

func awaitCall<T>(_ start: (@escaping (T) -> Void, @escaping (String) -> Void) -> Void) async throws -> T {
    try await withCheckedThrowingContinuation { continuation in
        start(
            { continuation.resume(returning: $0) },
            { continuation.resume(throwing: Failure(message: $0)) }
        )
    }
}

enum ServerBook {
    private static let key = "reelstash.servers"

    static var raw: String {
        get { UserDefaults.standard.string(forKey: key) ?? AppleServers.shared.empty() }
        set { UserDefaults.standard.set(newValue, forKey: key) }
    }
}

enum Gate {
    case starting
    case connect
    case signIn(url: String, info: ServerInfo)
    case locked
    case ready
}

@MainActor
final class AppModel: ObservableObject {
    @Published var gate: Gate = .starting
    @Published var libraries: [Library] = []
    @Published var libraryId: Int64 = -1
    @Published var notice: String?
    @Published private(set) var accountName = ""
    @Published private(set) var serverName = ""
    @Published private(set) var palette = ColorPalettes.grove

    private(set) var session: AppleSession?
    private var events: AppleCancel?
    private var raw: String = ServerBook.raw

    var servers: [SavedServer] { AppleServers.shared.list(raw: raw) }
    var current: SavedServer? { AppleServers.shared.current(raw: raw) }
    var library: Library? { libraries.first { $0.id == libraryId } }
    var entertainment: Bool { library?.isEntertainment ?? true }

    func headers() -> [String: String] {
        guard let session else { return [:] }
        var headers: [String: String] = [:]
        if let authorization = session.authorization() { headers["Authorization"] = authorization }
        if let unlock = session.unlockHeader() { headers["X-Reelstash-Unlock"] = unlock }
        if let library = session.libraryHeader() { headers["X-Library"] = library }
        return headers
    }

    func start() {
        guard session == nil else { return }
        if let saved = current {
            open(saved)
        } else {
            gate = .connect
        }
    }

    func probe(_ input: String) async throws -> (url: String, info: ServerInfo) {
        let address = AppleServersKt.normalizeServerAddress(input: input)
        guard let url = address.url else { throw Failure(message: address.error ?? "Enter your server's address") }
        let probe = AppleSession(baseUrl: url, token: nil, libraryId: -1, deviceName: DeviceLabel.name, platform: DeviceLabel.platform)
        do {
            let info = try await awaitCall { probe.serverInfo(onOk: $0, onErr: $1) }
            if let theme = try? await awaitCall({ probe.colorTheme(onOk: $0, onErr: $1) }) {
                applyColorTheme(theme)
            }
            probe.close()
            if let reason = AppleModelsKt.rejectServer(info: info) { throw Failure(message: reason) }
            return (url, info)
        } catch {
            probe.close()
            throw error
        }
    }

    func signedIn(url: String, name: String, session deviceSession: DeviceSession) {
        raw = AppleServers.shared.rememberSignIn(raw: raw, baseUrl: url, serverName: name, session: deviceSession)
        ServerBook.raw = raw
        if let saved = current { open(saved) }
    }

    func unlock(pin: String) async throws -> Bool {
        guard let session else { return false }
        let flag = try await awaitCall { session.unlock(pin: pin, onOk: $0, onErr: $1) }
        let ok = flag.boolValue
        if ok { gate = .ready }
        return ok
    }

    func selectLibrary(_ id: Int64) {
        guard let session, let saved = current else { return }
        libraryId = id
        session.setLibrary(libraryId: id)
        raw = AppleServers.shared.withLibrary(raw: raw, serverId: saved.id, libraryId: id)
        ServerBook.raw = raw
    }

    func switchTo(_ id: String) {
        raw = AppleServers.shared.select(raw: raw, id: id)
        ServerBook.raw = raw
        if let saved = current { open(saved) }
    }

    func addServer() { gate = .connect }

    func cancelAdd() {
        if let saved = current { open(saved) } else { gate = .connect }
    }

    func signOutCurrent() async {
        guard let saved = current else { return }
        let leaving = session
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            if let leaving {
                leaving.signOut { continuation.resume() }
            } else {
                continuation.resume()
            }
        }
        forget(saved.id)
    }

    private func forget(_ id: String) {
        events?.cancel()
        session?.close()
        session = nil
        raw = AppleServers.shared.remove(raw: raw, id: id)
        ServerBook.raw = raw
        libraries = []
        if let saved = current {
            open(saved)
        } else {
            gate = .connect
        }
    }

    private func open(_ saved: SavedServer) {
        events?.cancel()
        session?.close()
        let next = AppleSession(
            baseUrl: saved.baseUrl,
            token: saved.token,
            libraryId: saved.appleLibraryId(),
            deviceName: DeviceLabel.name,
            platform: DeviceLabel.platform
        )
        session = next
        libraryId = saved.appleLibraryId()
        serverName = saved.name
        accountName = saved.user.displayName
        events = next.observe { [weak self] event in
            Task { @MainActor in
                guard let self else { return }
                if event == "SignedOut" { self.forget(saved.id) }
                if event == "Locked" { self.gate = .locked }
            }
        }
        gate = .starting
        Task {
            do {
                let status = try await awaitCall { next.appLock(onOk: $0, onErr: $1) }
                let needsPin = status.hasPin && (status.locked || status.scope == "app")
                gate = needsPin ? .locked : .ready
                await refreshColorTheme()
                await refreshLibraries()
            } catch {
                notice = error.localizedDescription
                gate = .ready
            }
        }
    }

    func applyColorTheme(_ id: String) {
        let next = ColorPalettes.resolve(id)
        Rs.palette = next
        if palette != next { palette = next }
    }

    func refreshColorTheme() async {
        guard let session else { return }
        if let id = try? await awaitCall({ session.colorTheme(onOk: $0, onErr: $1) }) {
            applyColorTheme(id)
        }
    }

    func refreshLibraries() async {
        guard let session else { return }
        guard let page = try? await awaitCall({ session.libraries(onOk: $0, onErr: $1) }) else { return }
        libraries = page.items
        if libraryId < 0, let fallback = page.defaultLibraryId?.int64Value ?? page.items.first?.id {
            selectLibrary(fallback)
        }
    }
}

final class ImageCache {
    static let shared = NSCache<NSURL, UIImage>()
}

struct AuthImage: View {
    let url: URL?
    var fallback: URL? = nil
    var headers: [String: String]
    var initial: String

    @State private var image: UIImage?

    var body: some View {
        Color.clear
            .background(Rs.surface)
            .overlay {
                if let image {
                    Image(uiImage: image).resizable().scaledToFill()
                } else {
                    Text(String(initial.prefix(1)).uppercased())
                        .font(.title2.weight(.semibold))
                        .foregroundStyle(Rs.tertiary)
                }
            }
            .clipped()
            .task(id: "\(url?.absoluteString ?? "")|\(fallback?.absoluteString ?? "")") { await load() }
    }

    private func load() async {
        if let loaded = await fetch(url) {
            image = loaded
            return
        }
        if fallback?.absoluteString != url?.absoluteString, let loaded = await fetch(fallback) {
            image = loaded
        }
    }

    private func fetch(_ url: URL?) async -> UIImage? {
        guard let url else { return nil }
        if let cached = ImageCache.shared.object(forKey: url as NSURL) { return cached }
        var request = URLRequest(url: url)
        headers.forEach { request.setValue($1, forHTTPHeaderField: $0) }
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              let http = response as? HTTPURLResponse, http.statusCode < 400,
              let decoded = UIImage(data: data) else { return nil }
        ImageCache.shared.setObject(decoded, forKey: url as NSURL)
        return decoded
    }
}
