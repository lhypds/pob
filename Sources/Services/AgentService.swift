import AppKit

class AgentService {
    static let shared = AgentService()
    private init() {}

    func generatePlan(instruction: String, screenshotBase64: String, screenshot: NSImage? = nil, sessionId: String, planId: String) async -> String? {
        let messages: [[String: Any]] = [
            ["role": "system", "content": """
            You are a desktop automation planner. Given a task instruction and a screenshot of the current screen, \
            break the task into concrete, executable steps. Each step must describe a single UI interaction in \
            plain language: what element to target, and what action to perform on it (e.g. click, type, scroll, \
            drag, key press). Steps must be specific enough that an agent can execute them one by one without \
            ambiguity. Do not include meta-steps like "verify" or "confirm" unless they require a specific action.

            Respond with a JSON object in exactly this format:
            {
              "steps": [
                { "sequence": 1, "instruction": "...", "expectation": "..." },
                { "sequence": 2, "instruction": "...", "expectation": "..." }
              ]
            }

            Fields:
            - instruction: the concrete action to perform for this step.
            - expectation: the observable outcome that confirms this step succeeded (e.g. "button is highlighted", "dialog appears", "text field is focused").
            """],
            ["role": "user", "content": [
                ["type": "text", "text": "Task: \(instruction)"],
                ["type": "image_url", "image_url": ["url": "data:image/png;base64,\(screenshotBase64)"]],
            ] as [[String: Any]]],
        ]
        let result = await OpenAIClient.shared.chat(messages: messages, jsonMode: true)
        if let plan = result.contentText, !plan.isEmpty {
            var responseToSave: [String: Any] = result.success
                ? result.rawAssistantMessage
                : ["error": result.error ?? "Unknown error"]
            if let usage = result.usage { responseToSave["usage"] = usage }
            StorageService.shared.savePlan(plan, messages: messages, response: responseToSave, sessionId: sessionId, planId: planId, screenshot: screenshot)
            AppLogger.log("[\(sessionId)] Plan saved")
            return plan
        }
        return nil
    }

    func makeTools() -> [[String: Any]] {
        [
            [
                "type": "function",
                "function": [
                    "name": "move",
                    "description": "Nudge the cursor by a relative pixel offset in screenshot space. All coordinates are screenshot pixels (origin = top-left, x increases right, y increases down). The app converts to real screen coordinates — you never deal with screen-level positions. You will receive a new screenshot showing the updated cursor position.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal offset in screenshot pixels. Positive = right, negative = left."],
                            "dy": ["type": "number", "description": "Vertical offset in screenshot pixels. Positive = down, negative = up."],
                        ],
                        "required": ["dx", "dy"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "click",
                    "description": "Left-click at the current cursor position. Executes immediately; you will receive a screenshot after the click.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "rightClick",
                    "description": "Right-click at the current cursor position. Executes immediately.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "doubleClick",
                    "description": "Double-click at the current cursor position. Executes immediately.",
                    "parameters": [
                        "type": "object",
                        "properties": [:] as [String: Any],
                        "required": [] as [String],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "drag",
                    "description": "Drag from the current cursor position by (dx, dy) screenshot pixels. The cursor ends at the new position and you receive an updated screenshot.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal drag offset in screenshot pixels. Positive = right."],
                            "dy": ["type": "number", "description": "Vertical drag offset in screenshot pixels. Positive = down."],
                        ] as [String: Any],
                        "required": ["dx", "dy"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "scroll",
                    "description": "Scroll at the current cursor position. dy > 0 = scroll down, dy < 0 = scroll up, dx > 0 = scroll right.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "dx": ["type": "number", "description": "Horizontal scroll amount in pixels."],
                            "dy": ["type": "number", "description": "Vertical scroll amount in pixels. Positive = down."],
                        ] as [String: Any],
                        "required": ["dx", "dy"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "typeText",
                    "description": "Type text at the current keyboard focus.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "text": ["type": "string", "description": "The text to type."],
                        ] as [String: Any],
                        "required": ["text"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "keyPress",
                    "description": "Press a special key. Supported: return, tab, space, delete, escape, left, right, up, down, home, end, pageup, pagedown, f1–f12, cmd+a/c/v/x/z/w/s/t/r.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "key": ["type": "string", "description": "Key name, e.g. \"return\", \"escape\", \"cmd+v\"."],
                        ] as [String: Any],
                        "required": ["key"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "sleep",
                    "description": "Pause execution for a given number of milliseconds.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "milliseconds": ["type": "number", "description": "Number of milliseconds to sleep."],
                        ] as [String: Any],
                        "required": ["milliseconds"],
                    ] as [String: Any],
                ] as [String: Any],
            ],
            [
                "type": "function",
                "function": [
                    "name": "take_screenshot",
                    "description": "Capture a fresh screenshot of the current screen state without moving or clicking. The screenshot is saved to disk but is NOT sent to the AI — use this only when you need to record the screen state for logging purposes, not when you need to see the screen. Optionally crop to a region using (crop_x, crop_y, crop_width, crop_height) in screenshot pixels.",
                    "parameters": [
                        "type": "object",
                        "properties": [
                            "crop_x": ["type": "number", "description": "Left edge of the crop region in screenshot pixels."],
                            "crop_y": ["type": "number", "description": "Top edge of the crop region in screenshot pixels."],
                            "crop_width": ["type": "number", "description": "Width of the crop region in screenshot pixels."],
                            "crop_height": ["type": "number", "description": "Height of the crop region in screenshot pixels."],
                        ] as [String: Any],
                        "required": [] as [String],
                    ] as [String: Any],
                ] as [String: Any],
            ],
        ]
    }
}
