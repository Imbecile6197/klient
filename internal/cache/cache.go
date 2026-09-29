// Package cache is the encrypted offline store of messages.
//
// Everything sensitive (subjects, senders, headers, bodies) is sealed with
// AES-256-GCM under a random key kept in the desktop keyring. Message bodies
// are stored exactly as Proton sends them (still PGP-encrypted to the address
// key), so the zero-access property holds on disk too. Only IDs, times,
// conversation IDs and folder/label IDs are stored in the clear, so lists can
// be sorted and filtered without decrypting everything.
package cache

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ProtonMail/go-proton-api"
	_ "modernc.org/sqlite"
)

type Cache struct {
	mu   sync.Mutex
	db   *sql.DB
	aead cipher.AEAD
}

const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id     TEXT PRIMARY KEY,
	conv   TEXT NOT NULL DEFAULT '',
	time   INTEGER NOT NULL,
	labels TEXT NOT NULL,      -- ",0,5,10," for LIKE queries
	meta   BLOB NOT NULL,      -- sealed JSON of proton.MessageMetadata
	full   BLOB                -- sealed JSON of proton.Message (body still PGP-encrypted)
);
CREATE INDEX IF NOT EXISTS messages_time ON messages(time DESC);
CREATE INDEX IF NOT EXISTS messages_conv ON messages(conv);
CREATE TABLE IF NOT EXISTS kv (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL        -- sealed JSON
);`

// Open opens (or creates) the cache at path, sealed with key (32 bytes).
func Open(path string, key []byte) (*Cache, error) {
	if len(key) != 32 {
		return nil, errors.New("cache: key must be 32 bytes")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cache{db: db, aead: aead}, nil
}

func (c *Cache) Close() error { return c.db.Close() }

func (c *Cache) seal(v any) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

func (c *Cache) open(sealed []byte, v any) error {
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return errors.New("cache: corrupt record")
	}
	plain, err := c.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return fmt.Errorf("cache: cannot decrypt record: %w", err)
	}
	return json.Unmarshal(plain, v)
}

func labelsCol(ids []string) string { return "," + strings.Join(ids, ",") + "," }

// PutMetadata inserts or updates message metadata, keeping cached bodies.
func (c *Cache) PutMetadata(msgs ...proton.MessageMetadata) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range msgs {
		sealed, err := c.seal(m)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO messages(id, conv, time, labels, meta) VALUES(?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET conv=excluded.conv, time=excluded.time, labels=excluded.labels, meta=excluded.meta`,
			m.ID, m.ConversationID, m.Time, labelsCol(m.LabelIDs), sealed); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PutMessage stores a full message (body as received, PGP-encrypted).
func (c *Cache) PutMessage(m proton.Message) error {
	if err := c.PutMetadata(m.MessageMetadata); err != nil {
		return err
	}
	sealed, err := c.seal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.db.Exec(`UPDATE messages SET full=? WHERE id=?`, sealed, m.ID)
	return err
}

// Message returns a cached full message; its metadata is the latest known.
func (c *Cache) Message(id string) (proton.Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var meta, full []byte
	if err := c.db.QueryRow(`SELECT meta, full FROM messages WHERE id=?`, id).Scan(&meta, &full); err != nil || full == nil {
		return proton.Message{}, false
	}
	var m proton.Message
	if c.open(full, &m) != nil {
		return proton.Message{}, false
	}
	var md proton.MessageMetadata
	if c.open(meta, &md) == nil {
		m.MessageMetadata = md
	}
	return m, true
}

// HasBody reports whether the full message is cached.
func (c *Cache) HasBody(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var n int
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id=? AND full IS NOT NULL`, id).Scan(&n)
	return n > 0
}

func (c *Cache) query(q string, args ...any) ([]proton.MessageMetadata, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []proton.MessageMetadata
	for rows.Next() {
		var sealed []byte
		if err := rows.Scan(&sealed); err != nil {
			return nil, err
		}
		var m proton.MessageMetadata
		if c.open(sealed, &m) == nil {
			out = append(out, m)
		}
	}
	return out, rows.Err()
}

// List returns one page of a folder, newest first.
func (c *Cache) List(labelID string, page, pageSize int) ([]proton.MessageMetadata, error) {
	return c.query(`SELECT meta FROM messages WHERE labels LIKE ? ORDER BY time DESC LIMIT ? OFFSET ?`,
		"%,"+labelID+",%", pageSize, page*pageSize)
}

// Conversation returns the cached messages of a conversation, oldest first.
func (c *Cache) Conversation(conv string) ([]proton.MessageMetadata, error) {
	return c.query(`SELECT meta FROM messages WHERE conv=? ORDER BY time ASC`, conv)
}

// Search matches all words against subject, sender and recipients of the
// newest max messages in a folder (decrypting metadata in memory only).
func (c *Cache) Search(labelID string, match func(proton.MessageMetadata) bool, max int) ([]proton.MessageMetadata, error) {
	all, err := c.List(labelID, 0, max)
	if err != nil {
		return nil, err
	}
	var out []proton.MessageMetadata
	for _, m := range all {
		if match(m) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Delete removes messages (e.g. deleted on the server).
func (c *Cache) Delete(ids ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		if _, err := c.db.Exec(`DELETE FROM messages WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

// Prune keeps bodies only for the newest keep messages and drops metadata of
// messages older than the newest keepMeta, bounding disk use.
func (c *Cache) Prune(keepBodies, keepMeta int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.db.Exec(`UPDATE messages SET full=NULL WHERE id NOT IN (SELECT id FROM messages ORDER BY time DESC LIMIT ?)`, keepBodies); err != nil {
		return err
	}
	_, err := c.db.Exec(`DELETE FROM messages WHERE id NOT IN (SELECT id FROM messages ORDER BY time DESC LIMIT ?)`, keepMeta)
	return err
}

// Stats returns the number of cached messages and cached bodies.
func (c *Cache) Stats() (messages, bodies int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.db.QueryRow(`SELECT COUNT(*), COUNT(full) FROM messages`).Scan(&messages, &bodies)
	return
}

// Clear deletes everything.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.db.Exec(`DELETE FROM messages; DELETE FROM kv;`)
	return err
}

// Set stores a sealed value (user keys, addresses, labels…).
func (c *Cache) Set(key string, v any) error {
	sealed, err := c.seal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.db.Exec(`INSERT INTO kv(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, sealed)
	return err
}

// Get loads a value stored with Set; false if missing.
func (c *Cache) Get(key string, v any) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var sealed []byte
	if err := c.db.QueryRow(`SELECT value FROM kv WHERE key=?`, key).Scan(&sealed); err != nil {
		return false
	}
	return c.open(sealed, v) == nil
}
