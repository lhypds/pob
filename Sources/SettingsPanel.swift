import SwiftUI
import Cocoa

struct SettingsPanel: View {
    @Binding var isPresented: Bool
    @State private var apiKey = ""
    @State private var testStatus: TestStatus = .idle
    @State private var errorMessage = ""
    
    enum TestStatus {
        case idle
        case testing
        case success
        case failure
    }
    var body: some View {
        VStack(spacing: 16) {
            Text("Settings")
                .font(.title)
                .fontWeight(.bold)
            
            VStack(alignment: .leading, spacing: 8) {
                Text("OpenAI API Key")
                    .font(.headline)
                
                SecureField("Enter your OpenAI API key", text: $apiKey)
                    .textFieldStyle(.roundedBorder)
            }
            
            HStack(spacing: 12) {
                Button(action: testConnection) {
                    if testStatus == .testing {
                        ProgressView()
                            .scaleEffect(0.8)
                    } else {
                        Text("Test Connection")
                    }
                }
                .disabled(apiKey.isEmpty || testStatus == .testing)
                
                if testStatus == .success {
                    HStack(spacing: 4) {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundColor(.green)
                        Text("Connection successful")
                            .foregroundColor(.green)
                            .font(.caption)
                    }
                } else if testStatus == .failure {
                    HStack(spacing: 4) {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundColor(.red)
                        Text("Connection failed")
                            .foregroundColor(.red)
                            .font(.caption)
                    }
                }
            }
            
            if !errorMessage.isEmpty {
                Text(errorMessage)
                    .font(.caption)
                    .foregroundColor(.red)
                    .lineLimit(3)
            }
            
            Spacer()
            
            HStack(spacing: 12) {
                Button("Save") {
                    saveSettings()
                }
                .keyboardShortcut(.return, modifiers: [])
                
                Button("Cancel") {
                    isPresented = false
                }
            }
        }
        .padding(24)
        .frame(minWidth: 400, minHeight: 300)
        .onAppear {
            apiKey = SettingsService.shared.getAPIKey()
        }
    }
    
    private func testConnection() {
        testStatus = .testing
        errorMessage = ""
        
        Task {
            let result = await OpenAIClient.shared.testConnection(apiKey: apiKey)
            
            DispatchQueue.main.async {
                if result.success {
                    testStatus = .success
                    errorMessage = ""
                } else {
                    testStatus = .failure
                    errorMessage = result.error ?? "Unknown error"
                }
            }
        }
    }
    
    private func saveSettings() {
        _ = SettingsService.shared.setAPIKey(apiKey)
        isPresented = false
    }
}

// #Preview disabled - requires Xcode preview compilation support
