import Foundation

class OpenAIClient {
    static let shared = OpenAIClient()
    
    private let baseURL = "https://api.openai.com/v1"
    private var apiKey: String?
    
    private init() {
        apiKey = SettingsService.shared.getAPIKey()
    }
    
    // MARK: - Public Methods
    
    func testConnection(apiKey: String) async -> (success: Bool, error: String?) {
        let url = "\(baseURL)/models"
        
        var request = URLRequest(url: URL(string: url)!)
        request.httpMethod = "GET"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            
            if let httpResponse = response as? HTTPURLResponse {
                if httpResponse.statusCode == 200 {
                    return (true, nil)
                } else {
                    let error = String(data: data, encoding: .utf8) ?? "Unknown error"
                    return (false, "HTTP \(httpResponse.statusCode): \(error)")
                }
            }
            return (false, "Invalid response")
        } catch {
            return (false, error.localizedDescription)
        }
    }
    
    func analyzeScreenshot(imageBase64: String, prompt: String = "Describe what you see in this screenshot and identify any UI elements.") async -> (success: Bool, content: String, error: String?) {
        self.apiKey = SettingsService.shared.getAPIKey()
        guard let apiKey = apiKey else {
            return (false, "", "API key not configured")
        }
        
        let url = "\(baseURL)/chat/completions"
        
        var request = URLRequest(url: URL(string: url)!)
        request.httpMethod = "POST"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        
        let payload: [String: Any] = [
            "model": "gpt-4-vision-preview",
            "messages": [
                [
                    "role": "user",
                    "content": [
                        [
                            "type": "text",
                            "text": prompt
                        ],
                        [
                            "type": "image_url",
                            "image_url": [
                                "url": "data:image/png;base64,\(imageBase64)"
                            ]
                        ]
                    ]
                ]
            ],
            "max_tokens": 2000
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
                    } else {
                        return (false, "", "Unable to parse response")
                    }
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
