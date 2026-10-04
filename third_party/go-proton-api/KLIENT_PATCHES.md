# Local patches (Klient)

Copy of github.com/ProtonMail/go-proton-api at 882710fb04bbe615ba08e8b457e277009292ce49
(MIT, see LICENSE), used through a `replace` directive in the root go.mod.

Changes (marked `// klient patch`):

- `message_types.go`: `MessageMetadata.ConversationID` — the API already returns it.
- `message_types.go`: `MessageFilter.ConversationID` — lets `/mail/v4/messages` list one conversation.

- `contact.go`: `getContactEmailsImpl` no longer sends an empty `Email` query
  parameter, which made `GetAllContactEmails(ctx, "")` return no contacts.

- `message_send.go`: `UpdateDraft` always encrypts the body. It skipped
  encryption for an empty body and sent "", which the API rejects
  ("Body is required"), so drafts with only an attachment failed.

To update: copy the new upstream version over this directory and re-apply the two fields.

- `klient_extra.go` (new file): `SnoozeConversations`, `UnsnoozeConversations`,
  `CancelSend`, `SetAutoResponder`, `AutoResponder`, `SnoozedLabel`, `EmptyLabel`.
- `mail_settings_types.go`: `MailSettings.AutoResponder`.
- `message_send_types.go`: `SendDraftReq.DeliveryTime` (scheduled send).
- `user_types.go`: `User.UsedBaseSpace/MaxBaseSpace/UsedDriveSpace/MaxDriveSpace` (split storage).
