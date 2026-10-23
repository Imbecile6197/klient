import SwiftUI

/// Compact Mail / Airmail–style message row.
struct MessageRowView: View {
    let message: AppState.MessageRow
    let selected: Bool
    /// When browsing Scheduled / Snoozed, show countdown instead of relative date.
    var countdownMode: Bool = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Circle()
                .fill(message.unread ? KlientTheme.unreadDot : Color.clear)
                .frame(width: 7, height: 7)
                .padding(.top, 7)

            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(message.sender.isEmpty ? String(localized: "Unknown") : message.sender)
                        .font(.callout.weight(message.unread ? .semibold : .medium))
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    Spacer(minLength: 6)
                    if message.isSpam {
                        Image(systemName: "xmark.bin.fill")
                            .font(.caption2)
                            .foregroundStyle(.orange)
                            .help(
                                String(localized: "AI spam filter: ")
                                    + AppState.SpamDecision.categoryName(message.spamCategory)
                            )
                            .accessibilityLabel(
                                String(localized: "AI spam filter: ")
                                    + AppState.SpamDecision.categoryName(message.spamCategory)
                            )
                    }
                    if message.hasAttachments {
                        Image(systemName: "paperclip")
                            .font(.caption2)
                            .foregroundStyle(.tertiary)
                    }
                    if message.threadCount > 1 {
                        Text("\(message.threadCount)")
                            .font(.caption2.weight(.semibold))
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Color.secondary.opacity(0.18), in: Capsule())
                            .foregroundStyle(.secondary)
                    }
                    Text(countdownMode
                         ? KlientTheme.countdown(until: message.time)
                         : KlientTheme.relativeDate(message.time))
                        .font(.caption2.weight(message.unread ? .semibold : .regular))
                        .foregroundStyle(
                            countdownMode
                            ? AnyShapeStyle(KlientTheme.brand)
                            : (message.unread ? AnyShapeStyle(KlientTheme.unreadDot) : AnyShapeStyle(.tertiary))
                        )
                        .monospacedDigit()
                }
                Text(message.subject.isEmpty ? String(localized: "(no subject)") : message.subject)
                    .font(.callout)
                    .foregroundStyle(message.unread ? .primary : .secondary)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(message.sender), \(message.subject)")
    }
}
