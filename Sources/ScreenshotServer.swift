import Cocoa
import Network

// Minimal HTTP server on port 8033 that lets the Python MCP server request screenshots
// from Pob's own ScreenshotService instead of the system screencapture command.
//
// Endpoint: GET /screenshot[?x=<n>&y=<n>&w=<n>&h=<n>]
// Returns: PNG image/png response
// Crop params are in screen points (logical pixels), origin top-left.
class ScreenshotServer {
    static let shared = ScreenshotServer()
    private var listener: NWListener?

    private init() {}

    func start(port: UInt16) {
        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true

        guard let nwPort = NWEndpoint.Port(rawValue: port) else {
            AppLogger.log("ScreenshotServer: invalid port \(port)")
            return
        }

        do {
            listener = try NWListener(using: params, on: nwPort)
        } catch {
            AppLogger.log("ScreenshotServer: failed to create listener: \(error)")
            return
        }

        listener?.stateUpdateHandler = { state in
            if case .ready = state {
                AppLogger.log("ScreenshotServer: listening on port \(port)")
            } else if case .failed(let err) = state {
                AppLogger.log("ScreenshotServer: failed: \(err)")
            }
        }

        listener?.newConnectionHandler = { [weak self] conn in
            self?.handle(conn)
        }

        listener?.start(queue: .global(qos: .utility))
    }

    private func handle(_ connection: NWConnection) {
        connection.start(queue: .global(qos: .utility))
        connection.receive(minimumIncompleteLength: 1, maximumLength: 8192) { [weak self] data, _, _, _ in
            guard let self = self, let data = data, !data.isEmpty else {
                connection.cancel()
                return
            }

            let cropRect = Self.parseCrop(from: data)
            DispatchQueue.main.async {
                guard let window = NSApplication.shared.windows.first,
                      let (image, _) = ScreenshotService.shared.captureWindowContentAreaWithContext(window: window) else {
                    self.sendError(connection)
                    return
                }

                var finalImage: NSImage? = image
                if let rect = cropRect {
                    finalImage = ScreenshotService.shared.crop(image, to: rect)
                }

                guard let img = finalImage,
                      let tiff = img.tiffRepresentation,
                      let rep = NSBitmapImageRep(data: tiff),
                      let png = rep.representation(using: .png, properties: [:]) else {
                    self.sendError(connection)
                    return
                }

                self.sendPNG(connection, data: png)
            }
        }
    }

    // Parse crop query params x, y, w, h from raw HTTP request bytes.
    private static func parseCrop(from requestData: Data) -> CGRect? {
        guard let requestStr = String(data: requestData, encoding: .utf8),
              let firstLine = requestStr.components(separatedBy: "\r\n").first else { return nil }

        // "GET /screenshot?x=10&y=20&w=300&h=200 HTTP/1.1"
        guard let qIdx = firstLine.firstIndex(of: "?") else { return nil }
        let afterQ = firstLine.index(after: qIdx)
        guard let spIdx = firstLine[afterQ...].firstIndex(of: " ") else { return nil }
        let query = String(firstLine[afterQ..<spIdx])

        var p: [String: CGFloat] = [:]
        for part in query.components(separatedBy: "&") {
            let kv = part.components(separatedBy: "=")
            if kv.count == 2, let val = Double(kv[1]) {
                p[kv[0]] = CGFloat(val)
            }
        }

        guard let x = p["x"], let y = p["y"], let w = p["w"], let h = p["h"] else { return nil }
        return CGRect(x: x, y: y, width: w, height: h)
    }

    private func sendPNG(_ connection: NWConnection, data: Data) {
        let header = "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: \(data.count)\r\nConnection: close\r\n\r\n"
        var response = header.data(using: .utf8)!
        response.append(data)
        connection.send(content: response, completion: .contentProcessed { _ in connection.cancel() })
    }

    private func sendError(_ connection: NWConnection) {
        let response = "HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
        connection.send(content: response.data(using: .utf8), completion: .contentProcessed { _ in connection.cancel() })
    }
}
