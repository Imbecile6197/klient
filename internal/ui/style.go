package ui

import (
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Proton's brand violet as the application accent, plus the small visual
// touches of the list and reader. Colours come from libadwaita variables so
// light and dark styles both work.
const appCSS = `
:root {
	--accent-bg-color: #6d4aff;
	--accent-color: #6243e6;
}
@media (prefers-color-scheme: dark) {
	:root { --accent-color: #a995ff; }
}

.compose-button {
	padding: 10px 18px;
	font-weight: bold;
	border-radius: 12px;
}

.sidebar-pane {
	background: color-mix(in srgb, var(--accent-bg-color) 5%, var(--sidebar-bg-color));
}

.count-badge {
	background: color-mix(in srgb, var(--accent-bg-color) 16%, transparent);
	color: var(--accent-color);
	border-radius: 999px;
	padding: 0 8px;
	font-size: 0.8em;
	font-weight: bold;
	min-width: 12px;
}

.unread-bar {
	background: var(--accent-bg-color);
	min-width: 4px;
	border-radius: 2px;
}

.row-subject-unread { font-weight: bold; }

.attach-chip {
	background: color-mix(in srgb, currentColor 8%, transparent);
	border-radius: 6px;
	padding: 1px 8px;
	font-size: 0.85em;
}

.label-dot {
	min-width: 10px;
	min-height: 10px;
	border-radius: 5px;
	margin: 3px;
}

.message-card.unread {
	border-left: 4px solid var(--accent-bg-color);
}

.summary-card {
	background: linear-gradient(135deg,
		color-mix(in srgb, var(--accent-bg-color) 14%, var(--card-bg-color)),
		color-mix(in srgb, #2ec27e 12%, var(--card-bg-color)));
}

.storage-bar trough { min-height: 6px; border-radius: 3px; }
.storage-bar progress { min-height: 6px; border-radius: 3px; background: var(--accent-bg-color); }
.storage-bar.storage-warning progress { background: #e5a50a; }
.storage-bar.storage-full progress { background: #e01b24; }

.unsubscribe-button {
	background: color-mix(in srgb, var(--accent-bg-color) 14%, transparent);
	color: var(--accent-color);
	border-radius: 999px;
	padding: 4px 14px;
	font-weight: bold;
}
.unsubscribe-button:hover {
	background: color-mix(in srgb, var(--accent-bg-color) 24%, transparent);
}

.invite-card {
	background: linear-gradient(135deg,
		color-mix(in srgb, #3584e4 14%, var(--card-bg-color)),
		color-mix(in srgb, var(--accent-bg-color) 10%, var(--card-bg-color)));
}

/* Another account has unread mail: a dot on the account switcher. */
.accounts-unread > box {
	background: radial-gradient(circle at calc(100% - 3px) 3px, #e01b24 3px, transparent 4px);
}

.checking-bar {
	background: color-mix(in srgb, var(--accent-bg-color) 10%, transparent);
	padding: 6px 12px;
}

.provider-tile {
	border-radius: 16px;
	padding: 6px;
}

.provider-badge {
	background: var(--window-bg-color);
	border-radius: 999px;
	padding: 1px;
}

.remote-bar {
	background: color-mix(in srgb, #e5a50a 16%, transparent);
	border-radius: 8px;
	padding: 4px 8px;
}
`

var labelCSS = gtk.NewCSSProvider()

func loadAppCSS() {
	display := gdk.DisplayGetDefault()
	p := gtk.NewCSSProvider()
	p.LoadFromString(appCSS)
	gtk.StyleContextAddProviderForDisplay(display, p, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
	gtk.StyleContextAddProviderForDisplay(display, labelCSS, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}

// setLabelColors defines .label-color-N classes for the user's labels
// (Proton label IDs are not valid CSS class names, hence the index).
func setLabelColors(colors []string) {
	var sb strings.Builder
	for i, c := range colors {
		if c == "" || strings.ContainsAny(c, "{};") {
			continue
		}
		fmt.Fprintf(&sb, ".label-color-%d { background: %s; }\n", i, c)
	}
	labelCSS.LoadFromString(sb.String())
}
