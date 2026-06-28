import Cocoa
import Foundation
import Network

/// MCP SSE server on port 8032.
/// Implements the Model Context Protocol (JSON-RPC over HTTP+SSE) in Swift,
/// replacing the Python pob_mcp_server.py + the old ScreenshotServer (port 8033).
///
/// Endpoints:
///   GET  /sse              — SSE stream; server emits endpoint event, then JSON-RPC responses
///   POST /messages?sessionId=<uuid> — client sends JSON-RPC requests here
class MCPServer {
    static let shared = MCPServer()
    private var listener: NWListener?

    // sessionId → active SSE connection
    private var sessions: [String: NWConnection] = [:]
    private let sessionsLock = NSLock()

    private init() {}

    func start(port: UInt16) {
        let params = NWParameters.tcp
        params.allowLocalEndpointReuse = true

        guard let nwPort = NWEndpoint.Port(rawValue: port) else {
            AppLogger.log("MCPServer: invalid port \(port)")
            return
        }

        do {
            listener = try NWListener(using: params, on: nwPort)
        } catch {
            AppLogger.log("MCPServer: failed to create listener: \(error)")
            return
        }

        listener?.stateUpdateHandler = { state in
            if case .ready = state {
                AppLogger.log("MCPServer: listening on port \(port)")
            } else if case let .failed(err) = state {
                AppLogger.log("MCPServer: listener failed: \(err)")
            }
        }

        listener?.newConnectionHandler = { [weak self] conn in
            self?.handleConnection(conn)
        }

        listener?.start(queue: .global(qos: .utility))
    }

    // MARK: - Connection dispatch

    private func handleConnection(_ connection: NWConnection) {
        connection.start(queue: .global(qos: .utility))
        connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { [weak self] data, _, _, _ in
            guard let self = self, let data = data, !data.isEmpty else {
                connection.cancel()
                return
            }
            self.dispatch(connection: connection, data: data)
        }
    }

    private func dispatch(connection: NWConnection, data: Data) {
        guard let raw = String(data: data, encoding: .utf8),
              let firstLine = raw.components(separatedBy: "\r\n").first
        else {
            sendHTTP(connection, status: 400, body: Data())
            return
        }

        let parts = firstLine.split(separator: " ", maxSplits: 2)
        guard parts.count >= 2 else {
            sendHTTP(connection, status: 400, body: Data())
            return
        }

        let method = String(parts[0])
        let fullPath = String(parts[1])
        let (path, query) = parsePathAndQuery(fullPath)

        if method == "OPTIONS" {
            sendHTTP(connection, status: 200, body: Data())
            return
        }

        if method == "GET", path == "/sse" {
            handleSSE(connection)
            return
        }

        if method == "POST", path == "/messages" {
            let sessionId = query["sessionId"] ?? ""
            let body = extractBody(from: raw)
            handlePost(connection, sessionId: sessionId, rawBody: body)
            return
        }

        sendHTTP(connection, status: 404, body: Data())
    }

    // MARK: - SSE endpoint

    private func handleSSE(_ connection: NWConnection) {
        let sessionId = UUID().uuidString

        sessionsLock.lock()
        sessions[sessionId] = connection
        sessionsLock.unlock()

        connection.stateUpdateHandler = { [weak self] state in
            switch state {
            case .failed, .cancelled:
                self?.removeSession(sessionId)
            default:
                break
            }
        }

        let headers = [
            "HTTP/1.1 200 OK",
            "Content-Type: text/event-stream",
            "Cache-Control: no-cache",
            "Connection: keep-alive",
            "Access-Control-Allow-Origin: *",
            "", "",
        ].joined(separator: "\r\n")

        send(connection, data: headers.data(using: .utf8)!) { [weak self] ok in
            guard ok, let self = self else { return }
            let endpoint = "event: endpoint\ndata: /messages?sessionId=\(sessionId)\n\n"
            self.send(connection, data: endpoint.data(using: .utf8)!) { ok in
                if !ok { self.removeSession(sessionId) }
            }
            self.scheduleHeartbeat(connection: connection, sessionId: sessionId)
        }
    }

    private func scheduleHeartbeat(connection: NWConnection, sessionId: String) {
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 30) { [weak self] in
            guard let self = self else { return }
            self.sessionsLock.lock()
            let alive = self.sessions[sessionId] != nil
            self.sessionsLock.unlock()
            guard alive else { return }
            self.send(connection, data: ": ping\n\n".data(using: .utf8)!) { ok in
                if ok { self.scheduleHeartbeat(connection: connection, sessionId: sessionId) }
                else { self.removeSession(sessionId) }
            }
        }
    }

    private func removeSession(_ sessionId: String) {
        sessionsLock.lock()
        sessions.removeValue(forKey: sessionId)
        sessionsLock.unlock()
    }

    // MARK: - POST handler

    private func handlePost(_ connection: NWConnection, sessionId: String, rawBody: String) {
        // Acknowledge immediately — response arrives via SSE.
        sendHTTP(connection, status: 202, body: Data())

        guard let bodyData = rawBody.data(using: .utf8),
              let json = try? JSONSerialization.jsonObject(with: bodyData) as? [String: Any]
        else {
            AppLogger.log("MCPServer: bad JSON in POST body")
            return
        }

        let rpcMethod = json["method"] as? String ?? ""
        let params = json["params"] as? [String: Any] ?? [:]
        let requestId = json["id"] // nil for notifications

        // Notifications have no id and need no response.
        guard let reqId = requestId else { return }

        if rpcMethod == "tools/call", let toolName = params["name"] as? String {
            AppLogger.log("MCPServer: \(rpcMethod) → \(toolName)")
        } else {
            AppLogger.log("MCPServer: \(rpcMethod)")
        }

        sessionsLock.lock()
        let sseConn = sessions[sessionId]
        sessionsLock.unlock()

        guard let sseConnection = sseConn else {
            AppLogger.log("MCPServer: no SSE session \(sessionId)")
            return
        }

        let response = processRPC(method: rpcMethod, id: reqId, params: params)
        sendSSEMessage(sseConnection, object: response)
    }

    // MARK: - JSON-RPC dispatch

    private func processRPC(method: String, id: Any, params: [String: Any]) -> [String: Any] {
        switch method {
        case "initialize":
            return rpcResult(id: id, result: [
                "protocolVersion": "2024-11-05",
                "capabilities": ["tools": [:] as [String: Any]],
                "serverInfo": ["name": "pob", "version": "1.0.0"],
            ] as [String: Any])

        case "ping":
            return rpcResult(id: id, result: [:] as [String: Any])

        case "tools/list":
            return rpcResult(id: id, result: [
                "tools": [[
                    "name": "take_screenshot",
                    "description": "Capture a screenshot of the Pob window and return it as a PNG image. "
                        + "All crop parameters are optional. When all four are provided, only that region is "
                        + "captured. Coordinates are in screen points (logical pixels), origin at top-left.",
                    "inputSchema": [
                        "type": "object",
                        "properties": [
                            "crop_x": ["type": "integer", "description": "Left edge in screen points."],
                            "crop_y": ["type": "integer", "description": "Top edge in screen points."],
                            "crop_width": ["type": "integer", "description": "Width in screen points."],
                            "crop_height": ["type": "integer", "description": "Height in screen points."],
                        ] as [String: Any],
                    ] as [String: Any],
                ] as [String: Any]],
            ] as [String: Any])

        case "tools/call":
            let name = params["name"] as? String ?? ""
            let arguments = params["arguments"] as? [String: Any] ?? [:]
            if name == "take_screenshot" {
                return takeScreenshot(id: id, arguments: arguments)
            }
            return rpcError(id: id, code: -32601, message: "Unknown tool: \(name)")

        default:
            return rpcError(id: id, code: -32601, message: "Method not found: \(method)")
        }
    }

    // MARK: - take_screenshot tool

    private func takeScreenshot(id: Any, arguments: [String: Any]) -> [String: Any] {
        let cropRect: CGRect? = {
            guard let x = arguments["crop_x"] as? Int,
                  let y = arguments["crop_y"] as? Int,
                  let w = arguments["crop_width"] as? Int,
                  let h = arguments["crop_height"] as? Int else { return nil }
            return CGRect(x: x, y: y, width: w, height: h)
        }()

        var pngData: Data?
        let sem = DispatchSemaphore(value: 0)

        DispatchQueue.main.async {
            defer { sem.signal() }
            guard let window = NSApplication.shared.windows.first,
                  let (image, _) = ScreenshotService.shared.captureWindowContentAreaWithContext(window: window)
            else { return }

            var img: NSImage? = image
            if let rect = cropRect {
                img = ScreenshotService.shared.crop(image, to: rect)
            }

            guard let finalImg = img,
                  let tiff = finalImg.tiffRepresentation,
                  let rep = NSBitmapImageRep(data: tiff),
                  let png = rep.representation(using: .png, properties: [:])
            else { return }

            pngData = png
        }

        sem.wait()

        guard let png = pngData else {
            return rpcError(id: id, code: -32603, message: "Screenshot capture failed")
        }

        return rpcResult(id: id, result: [
            "content": [[
                "type": "image",
                "data": png.base64EncodedString(),
                "mimeType": "image/png",
            ] as [String: Any]],
        ] as [String: Any])
    }

    // MARK: - Helpers

    private func rpcResult(id: Any, result: Any) -> [String: Any] {
        ["jsonrpc": "2.0", "id": id, "result": result]
    }

    private func rpcError(id: Any, code: Int, message: String) -> [String: Any] {
        ["jsonrpc": "2.0", "id": id, "error": ["code": code, "message": message] as [String: Any]]
    }

    private func sendSSEMessage(_ connection: NWConnection, object: [String: Any]) {
        guard let data = try? JSONSerialization.data(withJSONObject: object),
              let jsonStr = String(data: data, encoding: .utf8) else { return }
        let event = "event: message\ndata: \(jsonStr)\n\n"
        send(connection, data: event.data(using: .utf8)!) { _ in }
    }

    private func sendHTTP(_ connection: NWConnection, status: Int, body: Data) {
        let statusText: String
        switch status {
        case 200: statusText = "OK"
        case 202: statusText = "Accepted"
        case 400: statusText = "Bad Request"
        case 404: statusText = "Not Found"
        default: statusText = "Error"
        }
        let header = [
            "HTTP/1.1 \(status) \(statusText)",
            "Content-Length: \(body.count)",
            "Access-Control-Allow-Origin: *",
            "Access-Control-Allow-Methods: GET, POST, OPTIONS",
            "Access-Control-Allow-Headers: Content-Type",
            "Connection: close",
            "", "",
        ].joined(separator: "\r\n")
        var response = header.data(using: .utf8)!
        response.append(body)
        connection.send(content: response, completion: .contentProcessed { _ in connection.cancel() })
    }

    /// Non-closing send; calls completion(true) on success, completion(false) on error.
    private func send(_ connection: NWConnection, data: Data, completion: @escaping (Bool) -> Void) {
        connection.send(content: data, completion: .contentProcessed { error in
            completion(error == nil)
        })
    }

    private func parsePathAndQuery(_ full: String) -> (path: String, query: [String: String]) {
        // Strip HTTP version suffix if present ("GET /sse HTTP/1.1" → "/sse")
        let stripped = full.components(separatedBy: " ").first ?? full
        guard let qIdx = stripped.firstIndex(of: "?") else {
            return (stripped, [:])
        }
        let path = String(stripped[..<qIdx])
        var query: [String: String] = [:]
        for part in stripped[stripped.index(after: qIdx)...].components(separatedBy: "&") {
            let kv = part.components(separatedBy: "=")
            if kv.count == 2 { query[kv[0]] = kv[1] }
        }
        return (path, query)
    }

    private func extractBody(from raw: String) -> String {
        guard let range = raw.range(of: "\r\n\r\n") else { return "" }
        return String(raw[range.upperBound...])
    }
}
