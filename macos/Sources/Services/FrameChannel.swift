import Foundation
import Network

/// The binary connection captured frames go down, instead of being base64'd
/// into a JSON-RPC response on the core's stdin.
///
/// A frame does not belong on that line. Base64 is a third again as many bytes
/// and JSON has to be parsed as one enormous string before any of it can be
/// used — but the real cost is that the line is shared. Every mouse move and
/// keystroke answers on it too, and behind a megabyte of picture they wait. At
/// one frame a second that is invisible; at thirty it is the whole difference
/// between a view you can work in and one you can only watch.
///
/// The core listens on loopback and sends the port and a token in a
/// frames.channel notification; this connects back and pushes. If the
/// connection is not up — not yet, or not any more — `send` says so and the
/// caller answers the old way, so a lost channel costs frame rate and nothing
/// else.
final class FrameChannel {
    /// Magic at the head of every frame, so a desynchronised stream is spotted
    /// rather than read as garbage lengths.
    private static let magic: [UInt8] = [0x50, 0x4F, 0x42, 0x46] // "POBF"

    private let queue = DispatchQueue(label: "pob.frames")
    private let lock = NSLock()
    private var connection: NWConnection?
    private var ready = false

    /// Connects to the core's frame channel. Called when frames.channel
    /// arrives, and again if the core ever re-offers one.
    func connect(port: UInt16, token: String) {
        guard let nwPort = NWEndpoint.Port(rawValue: port) else { return }
        stop()

        let connection = NWConnection(host: .ipv4(.loopback), port: nwPort, using: .tcp)
        lock.lock()
        self.connection = connection
        ready = false
        lock.unlock()

        connection.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            switch state {
            case .ready:
                // The token goes first, before any frame. It is not the
                // security boundary — the core listens on loopback only — but
                // it keeps anything else on this machine that happens to find
                // the port from feeding it frames.
                connection.send(content: Data((token + "\n").utf8), completion: .contentProcessed { error in
                    guard error == nil else {
                        self.stop()
                        return
                    }
                    self.lock.lock()
                    self.ready = true
                    self.lock.unlock()
                    AppLogger.log("FrameChannel: connected on port \(port)")
                })
            case .failed, .cancelled:
                self.lock.lock()
                self.ready = false
                self.lock.unlock()
            default:
                break
            }
        }
        connection.start(queue: queue)
    }

    func stop() {
        lock.lock()
        let existing = connection
        connection = nil
        ready = false
        lock.unlock()
        existing?.cancel()
    }

    /// Pushes one frame. Answers false if the channel is not up, which is the
    /// caller's cue to reply on the JSON-RPC line instead.
    ///
    /// The frame is built whole and sent in one call: TCP keeps the order, so
    /// the reader on the other end only has to read lengths, but a frame split
    /// across sends could interleave with the next one and there is no
    /// resynchronising a length-prefixed stream.
    func send(id: String, meta: [String: Any], payload: Data) -> Bool {
        lock.lock()
        let connection = self.connection
        let ready = self.ready
        lock.unlock()
        guard ready, let connection else { return false }

        guard let metaData = try? JSONSerialization.data(withJSONObject: meta) else { return false }
        let idData = Data(id.utf8)
        guard idData.count <= Int(UInt16.max) else { return false }

        var frame = Data()
        frame.reserveCapacity(payload.count + metaData.count + idData.count + 14)
        frame.append(contentsOf: Self.magic)
        frame.append(bigEndian16(UInt16(idData.count)))
        frame.append(idData)
        frame.append(bigEndian32(UInt32(metaData.count)))
        frame.append(metaData)
        frame.append(bigEndian32(UInt32(payload.count)))
        frame.append(payload)

        connection.send(content: frame, completion: .contentProcessed { [weak self] error in
            if error != nil { self?.stop() }
        })
        return true
    }

    private func bigEndian16(_ value: UInt16) -> Data {
        Data([UInt8(value >> 8), UInt8(value & 0xFF)])
    }

    private func bigEndian32(_ value: UInt32) -> Data {
        Data([
            UInt8((value >> 24) & 0xFF),
            UInt8((value >> 16) & 0xFF),
            UInt8((value >> 8) & 0xFF),
            UInt8(value & 0xFF),
        ])
    }
}
