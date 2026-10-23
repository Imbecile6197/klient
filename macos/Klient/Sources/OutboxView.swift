import SwiftUI

/// Outbox depth: retry / edit / delete (Linux outbox.go).
struct OutboxView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss

    @State private var confirmDeleteID: String?

    var body: some View {
        VStack(spacing: 0) {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(String(localized: "Outbox"))
                        .font(.title2.weight(.semibold))
                    Text(String(localized: "Messages that could not be sent. After a connection failure they are sent again automatically every minute; otherwise send them again, edit them or delete them."))
                        .font(.callout)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer()
                Button(String(localized: "Close")) { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(20)

            Divider()

            if state.outboxItems.isEmpty {
                ContentUnavailableView {
                    Label(String(localized: "The Outbox Is Empty"), systemImage: "paperplane")
                } description: {
                    Text(String(localized: "Messages that could not be sent"))
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                List {
                    ForEach(state.outboxItems) { item in
                        outboxRow(item)
                    }
                }
                .listStyle(.inset(alternatesRowBackgrounds: true))
            }
        }
        .frame(width: 620, height: 460)
        .onAppear { state.refreshOutbox() }
        .confirmationDialog(
            String(localized: "Delete the Message?"),
            isPresented: Binding(
                get: { confirmDeleteID != nil },
                set: { if !$0 { confirmDeleteID = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button(String(localized: "Delete"), role: .destructive) {
                if let id = confirmDeleteID {
                    state.deleteOutboxItem(id)
                }
                confirmDeleteID = nil
            }
            Button(String(localized: "Cancel"), role: .cancel) { confirmDeleteID = nil }
        } message: {
            Text(String(localized: "The message will not be sent and cannot be recovered."))
        }
    }

    @ViewBuilder
    private func outboxRow(_ item: AppState.OutboxItem) -> some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                Text(item.subject.isEmpty ? String(localized: "(no subject)") : item.subject)
                    .font(.headline)
                    .lineLimit(1)
                Text(String(localized: "To: ") + (item.to.isEmpty ? "—" : item.to)
                     + (item.email.isEmpty ? "" : " · \(item.email)"))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
                Text(statusText(item))
                    .font(.caption)
                    .foregroundStyle(item.waiting ? KlientTheme.brand : .secondary)
                    .lineLimit(3)
            }
            Spacer(minLength: 8)
            HStack(spacing: 6) {
                Button {
                    state.retryOutboxItem(item.id)
                } label: {
                    Image(systemName: "paperplane")
                }
                .help(String(localized: "Send Again"))
                .disabled(item.sending)

                Button {
                    dismiss()
                    state.editOutboxItem(item.id)
                } label: {
                    Image(systemName: "pencil")
                }
                .help(String(localized: "Edit"))
                .disabled(item.sending)

                Button(role: .destructive) {
                    confirmDeleteID = item.id
                } label: {
                    Image(systemName: "trash")
                }
                .help(String(localized: "Delete"))
                .disabled(item.sending)
            }
            .buttonStyle(.borderless)
            .controlSize(.small)
        }
        .padding(.vertical, 4)
    }

    private func statusText(_ item: AppState.OutboxItem) -> String {
        if item.sending {
            return String(localized: "Sending…")
        }
        if item.waiting {
            return String(localized: "Waiting for the connection – ") + item.error
        }
        return item.error
    }
}
