import Foundation

struct ToolCall {
    let id: String
    let name: String
    let arguments: [String: Any]
}

struct ChatResult {
    let success: Bool
    let contentText: String?
    let toolCalls: [ToolCall]
    // The raw assistant message dict ready to append back into the messages array.
    let rawAssistantMessage: [String: Any]
    let usage: [String: Any]?
    let error: String?
}

class OpenAIClient {
    static let shared = OpenAIClient()

    private init() {}

    /// Send a multi-turn conversation with optional tool definitions.
    func chat(messages: [[String: Any]], tools: [[String: Any]] = [], jsonMode: Bool = false) async -> ChatResult {
        let apiKey = SettingsService.shared.getAPIKey()
        guard !apiKey.isEmpty else {
            return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                              usage: nil, error: "API key not configured. Set openai_api_key in settings.json.")
        }

        let model = SettingsService.shared.getModel()
        let baseURL = SettingsService.shared.getBaseURL()

        var request = URLRequest(url: URL(string: "\(baseURL)/chat/completions")!)
        request.httpMethod = "POST"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        var payload: [String: Any] = [
            "model": model,
            "messages": messages,
        ]
        if !tools.isEmpty {
            payload["tools"] = tools
            payload["parallel_tool_calls"] = false
        }
        if jsonMode {
            payload["response_format"] = ["type": "json_object"]
        }

        do {
            request.httpBody = try JSONSerialization.data(withJSONObject: payload)
        } catch {
            return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                              usage: nil, error: "Failed to serialize request: \(error)")
        }

        do {
            let (data, response) = try await URLSession.shared.data(for: request)

            guard let http = response as? HTTPURLResponse else {
                return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                                  usage: nil, error: "Invalid response")
            }
            guard http.statusCode == 200 else {
                let body = String(data: data, encoding: .utf8) ?? "Unknown"
                return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                                  usage: nil, error: "HTTP \(http.statusCode): \(body)")
            }

            guard let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let choices = json["choices"] as? [[String: Any]],
                  let message = choices.first?["message"] as? [String: Any]
            else {
                return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                                  usage: nil, error: "Unexpected response format")
            }

            let usage = json["usage"] as? [String: Any]
            let contentText = message["content"] as? String

            var toolCalls: [ToolCall] = []
            if let rawCalls = message["tool_calls"] as? [[String: Any]] {
                for tc in rawCalls {
                    guard let id = tc["id"] as? String,
                          let fn = tc["function"] as? [String: Any],
                          let name = fn["name"] as? String,
                          let argsStr = fn["arguments"] as? String,
                          let argsData = argsStr.data(using: .utf8),
                          let args = (try? JSONSerialization.jsonObject(with: argsData)) as? [String: Any]
                    else {
                        continue
                    }
                    toolCalls.append(ToolCall(id: id, name: name, arguments: args))
                }
            }

            return ChatResult(success: true, contentText: contentText, toolCalls: toolCalls,
                              rawAssistantMessage: message, usage: usage, error: nil)
        } catch {
            return ChatResult(success: false, contentText: nil, toolCalls: [], rawAssistantMessage: [:],
                              usage: nil, error: error.localizedDescription)
        }
    }
}
