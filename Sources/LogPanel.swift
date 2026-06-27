import SwiftUI
import Foundation
import Cocoa

struct LogPanel: View {
    @Binding var isPresented: Bool
    @State private var logEntries: [LogEntry] = []
    @State private var selectedEntry: LogEntry?
    @State private var requestDetails: String = ""
    @State private var responseDetails: String = ""
    
    var body: some View {
        VStack(spacing: 0) {
            HStack {
                Text("Operation Log")
                    .font(.title)
                    .fontWeight(.bold)
                
                Spacer()
                
                Button(action: refreshLogs) {
                    Label("Refresh", systemImage: "arrow.clockwise")
                        .font(.caption)
                }
            }
            .padding(16)
            .borderBottom()
            
            HStack(spacing: 16) {
                // Log entries list
                VStack(alignment: .leading, spacing: 0) {
                    Text("Requests")
                        .font(.caption)
                        .fontWeight(.bold)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 8)
                    
                    List(logEntries, id: \.id) { entry in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(entry.formattedDate)
                                .font(.caption)
                                .foregroundColor(.secondary)
                            
                            Text(entry.status.uppercased())
                                .font(.caption2)
                                .fontWeight(.bold)
                                .foregroundColor(entry.status == "success" ? .green : .red)
                        }
                        .contentShape(Rectangle())
                        .onTapGesture {
                            selectedEntry = entry
                            loadEntryDetails(entry)
                        }
                        .background(selectedEntry?.id == entry.id ? Color.blue.opacity(0.2) : Color.clear)
                    }
                    .listStyle(.sidebar)
                    .frame(maxWidth: 250)
                }
                .frame(maxWidth: 250)
                .border(Color.gray.opacity(0.3), width: 1)
                
                // Details panel
                VStack(alignment: .leading, spacing: 12) {
                    if let selected = selectedEntry {
                        Text("Request Details for \(selected.formattedDate)")
                            .font(.headline)
                        
                        ScrollView {
                            Text(requestDetails)
                                .font(.caption)
                                .font(.system(.caption, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(maxHeight: 150)
                        .padding(8)
                        .background(Color.gray.opacity(0.1))
                        .cornerRadius(4)
                        
                        Text("Response Details")
                            .font(.headline)
                        
                        ScrollView {
                            Text(responseDetails)
                                .font(.caption)
                                .font(.system(.caption, design: .monospaced))
                                .textSelection(.enabled)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(maxHeight: 150)
                        .padding(8)
                        .background(Color.gray.opacity(0.1))
                        .cornerRadius(4)
                    } else {
                        VStack {
                            Text("Select a request to view details")
                                .font(.caption)
                                .foregroundColor(.secondary)
                            Spacer()
                        }
                    }
                    
                    Spacer()
                }
                .frame(maxWidth: .infinity)
                .padding(12)
            }
            .frame(maxHeight: .infinity)
            
            HStack {
                Spacer()
                Button("Close") {
                    isPresented = false
                }
            }
            .padding(12)
            .borderTop()
        }
        .frame(minWidth: 700, minHeight: 500)
        .onAppear {
            refreshLogs()
        }
    }
    
    private func refreshLogs() {
        logEntries = StorageService.shared.getLogEntries()
    }
    
    private func loadEntryDetails(_ entry: LogEntry) {
        let details = StorageService.shared.getEntryDetails(entry)
        requestDetails = details.request
        responseDetails = details.response
    }
}

struct LogEntry: Identifiable {
    let id: String
    let timestamp: Int
    let status: String
    
    var formattedDate: String {
        let date = Date(timeIntervalSince1970: TimeInterval(timestamp))
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .medium
        return formatter.string(from: date)
    }
}

extension View {
    func borderTop() -> some View {
        self.border(Color.gray.opacity(0.3), width: 1)
    }
    
    func borderBottom() -> some View {
        self.border(Color.gray.opacity(0.3), width: 1)
    }
}

// #Preview disabled - requires Xcode preview compilation support
