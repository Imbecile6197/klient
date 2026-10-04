// Package config loads and stores the client configuration in
// $XDG_CONFIG_HOME/klient/config.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Imbecile6197/klient/internal/rules"
)

const appName = "klient"

// Feed describes one downloadable spam/phishing blocklist.
type Feed struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// Kind: "spamhaus-drop-json" (IP CIDR ranges), "hostfile" (domains in
	// hosts-file format), "url-list" (one URL per line) or "domain-list".
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}

type Config struct {
	// Proton API
	ProtonHostURL    string `json:"proton_host_url"`
	ProtonAppVersion string `json:"proton_app_version"`

	// AI: provider ("claude", "openai", "gemini") and model per role.
	AssistantProvider string `json:"assistant_provider"`
	AssistantModel    string `json:"assistant_model"`
	SpamProvider      string `json:"spam_provider"`
	SpamModel         string `json:"spam_model"`
	// Maximum number of body characters sent to Claude for spam classification.
	SpamBodyChars int `json:"spam_body_chars"`
	// AI spam filter enabled at all.
	SpamFilterEnabled bool `json:"spam_filter_enabled"`
	// Messages whose spam probability is >= threshold are moved to Spam.
	SpamThreshold float64 `json:"spam_threshold"`

	// Senders (addresses or "@domain") whose remote images load automatically.
	RemoteContentSenders []string `json:"remote_content_senders"`

	// Seconds a sent message waits (with an "Undo" button) before it is sent.
	SendDelay int `json:"send_delay"`

	// New inbox mail stays hidden until the spam filter has judged it.
	HoldUntilChecked bool `json:"hold_until_checked"`

	// Filters applied to incoming mail.
	Rules []rules.Rule `json:"rules"`

	// Newest messages kept (with bodies) in the encrypted offline cache; 0 = off.
	OfflineMessages int `json:"offline_messages"`

	// Group messages into Proton conversations.
	Threads bool `json:"threads"`

	// Signature appended to new messages ("" = none).
	Signature string `json:"signature"`
	// HTML signature, used instead of Signature when SignatureUseHTML is on.
	SignatureHTML    string `json:"signature_html,omitempty"`
	SignatureUseHTML bool   `json:"signature_use_html,omitempty"`

	// Logged-in Proton accounts (login names, in sidebar order) and the one
	// shown in the window.
	Accounts      []string `json:"accounts"`
	ActiveAccount string   `json:"active_account"`

	// IMAP/SMTP accounts (Seznam, Gmail, others); listed in Accounts as
	// "imap:<address>".
	MailServers []MailServer `json:"mail_servers"`

	// Keep running (tray icon, notifications) after the window is closed.
	RunInBackground bool `json:"run_in_background"`

	// Attach the sender's public key to outgoing mail by default.
	AttachPublicKey bool `json:"attach_public_key"`

	// AI: label new mail with the best fitting user label.
	AutoLabel bool `json:"auto_label"`
	// AI: morning overview of unread mail at DigestHour (local time).
	Digest     bool   `json:"digest"`
	DigestHour int    `json:"digest_hour"`
	DigestLast string `json:"digest_last"` // date of the last overview, 2006-01-02

	// Look up recipients' PGP keys on keys.openpgp.org (IMAP accounts; WKD of
	// the recipient's own domain is always asked).
	KeyServerLookup bool `json:"key_server_lookup"`

	// Never send anything to cloud AI providers (only the local Ollama).
	LocalOnly bool `json:"local_only"`
	// Keep the managed Ollama and its models up to date.
	OllamaAutoUpdate bool   `json:"ollama_auto_update"`
	OllamaLastCheck  string `json:"ollama_last_check"` // RFC 3339

	// Version that ran last, to show "What's New" after an update.
	LastRunVersion string `json:"last_run_version"`

	// Interface language: "" follows the system, or "en", "cs".
	Language string `json:"language"`

	// Klient updates from the GitHub releases.
	AutoUpdate bool `json:"auto_update"`
	// The model last chosen for each role and provider ("spam:gemini"), so
	// switching providers back and forth keeps it.
	RoleModels map[string]string `json:"role_models,omitempty"`
	// Folders left out of the sidebar, per account (Username -> folder IDs).
	HiddenFolders map[string][]string `json:"hidden_folders,omitempty"`
	// Messages older than this many days are deleted from Trash and Spam
	// (0 = never).
	AutoEmptyDays int `json:"auto_empty_days,omitempty"`
	// Buttons of the new-mail notification ("read", "archive", "trash",
	// "spam"); nil = the defaults. No omitempty: an empty list is a choice.
	NotifyButtons []string `json:"notify_buttons"`
	// Underline misspelled words while writing (system dictionaries).
	SpellCheck      bool   `json:"spell_check"`
	UpdateLastCheck string `json:"update_last_check"` // RFC 3339
	// Summarise new mail in advance with the local model (on AC power only).
	PrecomputeSummaries bool `json:"precompute_summaries"`

	// Blocklist feeds and refresh interval.
	Feeds          []Feed   `json:"feeds"`
	UpdateInterval Duration `json:"update_interval"`
}

// MailServer is the configuration of an IMAP/SMTP account.
type MailServer struct {
	Kind         string `json:"kind"` // "seznam", "gmail", "imap"
	Email        string `json:"email"`
	Name         string `json:"name"` // sender name
	Username     string `json:"username"`
	IMAPHost     string `json:"imap_host"`
	IMAPPort     int    `json:"imap_port"`
	IMAPSecurity string `json:"imap_security"` // "ssl" or "starttls"
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPSecurity string `json:"smtp_security"`
	// Certificates the user trusted although they failed verification
	// (expired, self-signed…): "host:port" -> SHA-256 fingerprint.
	TrustedCerts map[string]string `json:"trusted_certs,omitempty"`
}

// ID is the account key used in Accounts and the keyring.
func (m MailServer) ID() string { return "imap:" + strings.ToLower(m.Email) }

// Duration marshals as a Go duration string ("12h").
type Duration struct{ time.Duration }

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func Default() Config {
	return Config{
		ProtonHostURL:       "https://mail.proton.me/api",
		ProtonAppVersion:    "Other",
		AssistantProvider:   "claude",
		AssistantModel:      "claude-opus-5",
		SpamProvider:        "claude",
		SpamModel:           "claude-opus-5",
		SpamBodyChars:       6000,
		SpamFilterEnabled:   true,
		SpamThreshold:       0.7,
		Threads:             true,
		OfflineMessages:     500,
		SendDelay:           10,
		RunInBackground:     true,
		HoldUntilChecked:    true,
		OllamaAutoUpdate:    true,
		AutoUpdate:          true,
		SpellCheck:          true,
		PrecomputeSummaries: true,
		DigestHour:          7,
		UpdateInterval:      Duration{12 * time.Hour},
		Feeds: []Feed{
			{Name: "Spamhaus DROP (IPv4)", URL: "https://www.spamhaus.org/drop/drop_v4.json", Kind: "spamhaus-drop-json", Enabled: true},
			{Name: "Spamhaus DROP (IPv6)", URL: "https://www.spamhaus.org/drop/drop_v6.json", Kind: "spamhaus-drop-json", Enabled: true},
			{Name: "URLhaus (abuse.ch)", URL: "https://urlhaus.abuse.ch/downloads/hostfile/", Kind: "hostfile", Enabled: true},
			{Name: "OpenPhish", URL: "https://raw.githubusercontent.com/openphish/public_feed/refs/heads/main/feed.txt", Kind: "url-list", Enabled: true},
		},
	}
}

func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, appName)
}

func DataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, appName)
	}
	return filepath.Join(os.Getenv("HOME"), ".local", "share", appName)
}

func path() string { return filepath.Join(ConfigDir(), "config.json") }

// Load reads the config file, creating it with defaults if it is missing.
func Load() (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path())
	if errors.Is(err, os.ErrNotExist) {
		return cfg, Save(cfg)
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Default(), err
	}
	// Configs written before multi-provider support.
	if cfg.AssistantProvider == "" {
		cfg.AssistantProvider = "claude"
	}
	if cfg.SpamProvider == "" {
		cfg.SpamProvider = "claude"
	}
	return cfg, nil
}

func Save(cfg Config) error {
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(), b, 0o600)
}
