import Shared
import SwiftUI

struct ConnectView: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.horizontalSizeClass) private var size
    @State private var address = ""
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 28) {
            VStack(spacing: 8) {
                Text("ReelStash")
                    .font(Rs.display(size == .regular ? 56 : 40))
                Text("Sign in to a server you run yourself.")
                    .font(Rs.text(16))
                    .foregroundStyle(Rs.secondary)
                    .multilineTextAlignment(.center)
            }
            GlassEffectContainer(spacing: 14) {
                VStack(alignment: .leading, spacing: 14) {
                    TextField("nas.local:8000", text: $address)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                        .brandField()
                    if let error {
                        Text(error).font(Rs.text(13)).foregroundStyle(Rs.accent)
                    }
                Button { Task { await check() } } label: {
                    Text(busy ? "Looking…" : "Continue").frame(maxWidth: .infinity)
                }
                .rsProminent()
                        .disabled(busy || address.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            if model.current != nil {
                Button("Back") { model.cancelAdd() }
                    .foregroundStyle(Rs.secondary)
            }
        }
        .padding(28)
        .frame(maxWidth: size == .regular ? 480 : .infinity)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func check() async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            let found = try await model.probe(address)
            model.gate = .signIn(url: found.url, info: found.info)
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct SignInView: View {
    @EnvironmentObject private var model: AppModel
    let url: String
    let info: ServerInfo

    @State private var username = ""
    @State private var password = ""
    @State private var error: String?
    @State private var busy = false
    @State private var pairing: PairingStart?
    @State private var pairingTask: Task<Void, Never>?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                VStack(alignment: .leading, spacing: 6) {
                    Text(info.name).font(Rs.display(40))
                    Text(url).font(Rs.text(14)).foregroundStyle(Rs.secondary)
                }
                if info.passwordLogin {
                    VStack(spacing: 12) {
                        TextField("Username", text: $username)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .brandField()
                        SecureField("Password", text: $password).brandField()
                        Button { Task { await passwordSignIn() } } label: {
                            Text(busy ? "Signing in…" : "Sign in").frame(maxWidth: .infinity)
                        }
                        .rsProminent()
                            .disabled(busy || username.isEmpty || password.isEmpty)
                    }
                }
                if info.pairing {
                    pairingBlock
                }
                if let error {
                    Text(error).font(.footnote).foregroundStyle(Rs.accent)
                }
                Button("Different server") {
                    pairingTask?.cancel()
                    model.gate = .connect
                }
                .foregroundStyle(Rs.secondary)
            }
            .padding(28)
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
        }
        .onAppear {
            if !info.passwordLogin && info.pairing { startPairing() }
        }
        .onDisappear { pairingTask?.cancel() }
    }

    @ViewBuilder private var pairingBlock: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Or pair this \(DeviceLabel.name)").font(.headline)
            Text("Enter the code on the web, under Settings → Devices, or at /link.")
                .font(.subheadline)
                .foregroundStyle(Rs.secondary)
            if let pairing {
                Text(pairing.userCode)
                    .font(Rs.display(48))
                    .monospaced()
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 8)
                Text("Waiting for approval…")
                    .font(.footnote)
                    .foregroundStyle(Rs.tertiary)
            } else {
                Button("Show a pairing code") { startPairing() }
                    .rsGlassControl()
            }
        }
        .padding(16)
        .rsGlass(in: .rect(cornerRadius: 22))
    }

    private func passwordSignIn() async {
        busy = true
        error = nil
        defer { busy = false }
        let client = AppleSession(baseUrl: url, token: nil, libraryId: -1, deviceName: DeviceLabel.name, platform: DeviceLabel.platform)
        do {
            let deviceSession = try await awaitCall { client.passwordSignIn(username: username, password: password, onOk: $0, onErr: $1) }
            client.close()
            model.signedIn(url: url, name: info.name, session: deviceSession)
        } catch {
            client.close()
            self.error = error.localizedDescription
        }
    }

    private func startPairing() {
        pairingTask?.cancel()
        error = nil
        let client = AppleSession(baseUrl: url, token: nil, libraryId: -1, deviceName: DeviceLabel.name, platform: DeviceLabel.platform)
        pairingTask = Task {
            do {
                let start = try await awaitCall { client.startPairing(onOk: $0, onErr: $1) }
                pairing = start
                let deadline = Date().addingTimeInterval(TimeInterval(start.expiresIn))
                while Date() < deadline {
                    try await Task.sleep(for: .seconds(max(2, start.interval)))
                    if Task.isCancelled { break }
                    let poll = try await awaitCall { client.pollPairing(deviceCode: start.deviceCode, onOk: $0, onErr: $1) }
                    if let session = poll.session {
                        client.close()
                        model.signedIn(url: url, name: info.name, session: session)
                        return
                    }
                }
                client.close()
                pairing = nil
                error = "That code expired. Show a new one."
            } catch {
                client.close()
                if !Task.isCancelled { self.error = error.localizedDescription }
            }
        }
    }
}

struct LockView: View {
    @EnvironmentObject private var model: AppModel
    @State private var pin = ""
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 18) {
            Image(systemName: "lock.fill").font(.largeTitle).foregroundStyle(Rs.accent)
            Text("Enter your PIN").font(.title2.weight(.semibold))
            SecureField("PIN", text: $pin)
                .keyboardType(.numberPad)
                .brandField()
                .frame(maxWidth: 280)
            if let error { Text(error).font(Rs.text(13)).foregroundStyle(Rs.accent) }
            Button { Task { await unlock() } } label: {
                Text(busy ? "Checking…" : "Unlock").frame(maxWidth: .infinity)
            }
            .rsProminent()
                .disabled(busy || pin.isEmpty)
                .frame(maxWidth: 280)
        }
        .padding(28)
    }

    private func unlock() async {
        busy = true
        error = nil
        defer { busy = false }
        do {
            let ok = try await model.unlock(pin: pin)
            if !ok { error = "That PIN is not right." }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @State private var busy = false

    var body: some View {
        List {
            Section("This device") {
                LabeledContent("Signed in", value: model.accountName)
                LabeledContent("Server", value: model.serverName)
            }
            Section("Library") {
                Button("All libraries") { model.selectLibrary(-1) }
                ForEach(model.libraries, id: \.id) { library in
                    Button {
                        model.selectLibrary(library.id)
                    } label: {
                        HStack {
                            Text(library.name)
                            if library.id == model.libraryId {
                                Spacer()
                                Image(systemName: "checkmark").foregroundStyle(Rs.accent)
                            }
                        }
                    }
                }
            }
            if model.servers.count > 1 {
                Section("Servers") {
                    ForEach(model.servers, id: \.id) { server in
                        Button(server.name) { model.switchTo(server.id) }
                    }
                }
            }
            Section {
                Button("Add a server") { model.addServer() }
                Button(busy ? "Signing out…" : "Sign out", role: .destructive) {
                    Task {
                        busy = true
                        await model.signOutCurrent()
                        busy = false
                    }
                }
            }
        }
        .navigationTitle("Settings")
        .toolbarTitleDisplayMode(.inlineLarge)
    }
}
