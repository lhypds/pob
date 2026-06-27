import Foundation

class OpenAIClient {
    static let shared = OpenAIClient()

    private let baseURL = "https://api.openai.com/v1"

    private init() {}

    func analyzeScreenshot(imageBase64: String, prompt: String) async -> (success: Bool, content: String, error: String?) {
        let apiKey = SettingsService.shared.getAPIKey()
        guard !apiKey.isEmpty else {
            return (false, "", "API key not configured. Set OPENAI_API_KEY in .env file.")
        }

        let model = SettingsService.shared.getModel()
        let maxTokens = SettingsService.shared.getMaxTokens()

        var request = URLRequest(url: URL(string: "\(baseURL)/chat/completions")!)
        request.httpMethod = "POST"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")

        let payload: [String: Any] = [
            "model": model,
            "messages": [
                [
                    "role": "user",
                    "content": [
                        ["type": "text", "text": prompt],
                        ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(imageBase64)"]]
                    ]
                ]
            ],
            "max_tokens": maxTokens
        ]

        do {
            request.httpBody = try JSONSerialization.data(withJSONObject: payload)
        } catch {
            return (false, "", "Failed to serialize request")
        }

        do {
            let (data, response) = try await URLSession.shared.data(for: request)

            if let httpResponse = response as? HTTPURLResponse {
                if httpResponse.statusCode == 200 {
                    if let jsonResponse = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                       let choices = jsonResponse["choices"] as? [[String: Any]],
                       let firstChoice = choices.first,
                       let message = firstChoice["message"] as? [String: Any],
                       let content = message["content"] as? String {
                        return (true, content, nil)
                    }
                    return (false, "", "Unable to parse response")
                } else {
                    let error = String(data: data, encoding: .utf8) ?? "Unknown error"
                    return (false, "", "HTTP \(httpResponse.statusCode): \(error)")
                }
            }
            return (false, "", "Invalid response")
        } catch {
            return (false, "", error.localizedDescription)
        }
    }
}
