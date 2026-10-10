import AVFoundation
import AVKit
import Shared
import SwiftUI

struct Cue {
    let start: Double
    let end: Double
    let text: String
}

@MainActor
final class Playback: ObservableObject {
    @Published var cue = ""
    @Published var reason = ""
    @Published var error: String?
    let player = AVPlayer()

    private var watch: WatchProgress?
    private var cues: [Cue] = []
    private var observer: Any?
    private var mediaId: Int64 = 0
    private var session: AppleSession?
    private var duration: Double = 0

    func start(request: PlayRequest, session: AppleSession, headers: [String: String], force: Bool) async {
        self.session = session
        mediaId = request.id
        error = nil
        do {
            let plan = try await awaitCall { session.preparePlayback(mediaId: request.id, forceTranscode: force, onOk: $0, onErr: $1) }
            reason = plan.reason
            guard let url = URL(string: plan.url) else { throw Failure(message: "The server did not return a playable address.") }
            let asset = AVURLAsset(url: url, options: ["AVURLAssetHTTPHeaderFieldsKey": headers])
            let item = AVPlayerItem(asset: asset)
            player.replaceCurrentItem(with: item)
            try? AVAudioSession.sharedInstance().setCategory(.playback, mode: .moviePlayback)
            try? AVAudioSession.sharedInstance().setActive(true)
            if request.resume > 1 {
                await player.seek(to: CMTime(seconds: request.resume, preferredTimescale: 600))
            }
            watch = WatchProgress(startPosition: request.resume)
            if request.resume > 1 { watch?.seeked(positionSeconds: request.resume) }
            duration = request.resume
            observe()
            player.play()
            if let track = plan.initialSubtitle {
                await loadSubtitles(session.subtitleUrl(mediaId: request.id, trackId: track.id, fileId: plan.fileId), headers: headers)
            } else {
                cues = []
                cue = ""
            }
            watchItem(item, session: session, headers: headers, request: request, alreadyForced: force || plan.kind == StreamKind.hls)
        } catch {
            self.error = error.localizedDescription
        }
    }

    func stop() {
        if let observer { player.removeTimeObserver(observer) }
        observer = nil
        flush(completed: finished)
        player.pause()
    }

    private var finished: Bool {
        duration > 0 && (watch?.position ?? 0) >= duration - WatchProgress.companion.END_MARGIN
    }

    private func observe() {
        if let observer { player.removeTimeObserver(observer) }
        observer = player.addPeriodicTimeObserver(forInterval: CMTime(seconds: 1, preferredTimescale: 2), queue: .main) { [weak self] time in
            guard let self else { return }
            Task { @MainActor in self.tick(time.seconds) }
        }
    }

    private func tick(_ seconds: Double) {
        guard seconds.isFinite else { return }
        if let itemDuration = player.currentItem?.duration.seconds, itemDuration.isFinite, itemDuration > 0 {
            duration = itemDuration
        }
        cue = cues.first { seconds >= $0.start && seconds < $0.end }?.text ?? ""
        guard let watch else { return }
        if watch.tick(positionSeconds: seconds) {
            watch.markSaved()
            flush(completed: finished)
        }
    }

    private func flush(completed: Bool) {
        guard let session, let watch else { return }
        session.reportProgress(
            mediaId: mediaId,
            position: watch.position,
            watched: watch.watchedSeconds,
            duration: duration,
            completed: completed,
            onErr: { _ in }
        )
    }

    private func loadSubtitles(_ urlString: String, headers: [String: String]) async {
        guard let url = URL(string: urlString) else { return }
        var request = URLRequest(url: url)
        headers.forEach { request.setValue($1, forHTTPHeaderField: $0) }
        guard let (data, response) = try? await URLSession.shared.data(for: request),
              let http = response as? HTTPURLResponse, http.statusCode < 400,
              let text = String(data: data, encoding: .utf8) else { return }
        cues = parseVTT(text)
    }

    private func watchItem(_ item: AVPlayerItem, session: AppleSession, headers: [String: String], request: PlayRequest, alreadyForced: Bool) {
        NotificationCenter.default.addObserver(forName: .AVPlayerItemFailedToPlayToEndTime, object: item, queue: .main) { [weak self] _ in
            guard let self, !alreadyForced else { return }
            Task { @MainActor in await self.start(request: request, session: session, headers: headers, force: true) }
        }
    }
}

struct PlayerScreen: View {
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    let request: PlayRequest
    @StateObject private var playback = Playback()

    var body: some View {
        ZStack(alignment: .bottom) {
            SystemPlayer(player: playback.player).ignoresSafeArea()
            if !playback.cue.isEmpty {
                Text(playback.cue)
                    .font(.title3.weight(.semibold))
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 18)
                    .padding(.vertical, 10)
                    .background(.black.opacity(0.55), in: RoundedRectangle(cornerRadius: 10))
                    .padding(.horizontal, 48)
                    .padding(.bottom, 78)
            }
        }
        .background(.black)
        .overlay(alignment: .topLeading) {
            HStack(spacing: 12) {
                Button { dismiss() } label: {
                    Image(systemName: "xmark")
                        .font(.body.weight(.semibold))
                        .padding(12)
                        .background(.black.opacity(0.45), in: Circle())
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(request.title).font(.headline).lineLimit(1)
                    if !playback.reason.isEmpty {
                        Text(playback.reason).font(.caption).foregroundStyle(.white.opacity(0.75))
                    }
                }
                Spacer()
            }
            .padding(16)
            .foregroundStyle(.white)
        }
        .overlay(alignment: .center) {
            if let error = playback.error {
                Text(error).multilineTextAlignment(.center).padding().foregroundStyle(.white)
            }
        }
        .task {
            guard let session = model.session else { return }
            await playback.start(request: request, session: session, headers: model.headers(), force: false)
        }
        .onDisappear { playback.stop() }
    }
}

struct SystemPlayer: UIViewControllerRepresentable {
    let player: AVPlayer

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.allowsPictureInPicturePlayback = true
        controller.canStartPictureInPictureAutomaticallyFromInline = true
        controller.entersFullScreenWhenPlaybackBegins = false
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player { controller.player = player }
    }
}

func parseVTT(_ raw: String) -> [Cue] {
    var cues: [Cue] = []
    let blocks = raw.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n\n")
    for block in blocks {
        let lines = block.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        guard let timing = lines.first(where: { $0.contains("-->") }) else { continue }
        let parts = timing.components(separatedBy: "-->")
        guard parts.count >= 2 else { continue }
        let start = vttTime(parts[0])
        let end = vttTime(parts[1])
        let text = lines.drop { !$0.contains("-->") }.dropFirst().joined(separator: "\n")
            .replacingOccurrences(of: "<[^>]+>", with: "", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if !text.isEmpty { cues.append(Cue(start: start, end: end, text: text)) }
    }
    return cues
}

private func vttTime(_ raw: String) -> Double {
    let stamp = raw.trimmingCharacters(in: .whitespaces).components(separatedBy: .whitespaces).first ?? ""
    let pieces = stamp.replacingOccurrences(of: ",", with: ".").split(separator: ":").map { Double($0) ?? 0 }
    if pieces.count == 3 { return pieces[0] * 3600 + pieces[1] * 60 + pieces[2] }
    if pieces.count == 2 { return pieces[0] * 60 + pieces[1] }
    return pieces.first ?? 0
}
