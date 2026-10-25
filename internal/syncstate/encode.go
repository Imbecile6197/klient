package syncstate

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The stored form is text that survives being a mail body:
//
//	KLIENT-SYNC <format> <sha256 of the payload>
//	<base64 of the gzipped JSON, in lines of 76 characters>
//	KLIENT-SYNC END
//
// The checksum catches a cut or damaged copy; such a copy is not used.

const magic = "KLIENT-SYNC"

var (
	// ErrCorrupt: the data is damaged or not Klient's.
	ErrCorrupt = errors.New("syncstate: damaged data")
	// ErrNewer: a newer Klient wrote the data; it is read, never overwritten.
	ErrNewer = errors.New("syncstate: made by a newer Klient")
)

// Encode turns s into its stored form.
func Encode(s *State) ([]byte, error) {
	js, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if _, err := w.Write(js); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(gz.Bytes())
	b64 := base64.StdEncoding.EncodeToString(gz.Bytes())
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s %d %s\r\n", magic, s.Format, hex.EncodeToString(sum[:]))
	for len(b64) > 76 {
		out.WriteString(b64[:76] + "\r\n")
		b64 = b64[76:]
	}
	out.WriteString(b64 + "\r\n")
	out.WriteString(magic + " END\r\n")
	return out.Bytes(), nil
}

// Decode reads the stored form, which may be surrounded by other text (the
// explanation in the mail). With ErrNewer the state is returned as well.
func Decode(b []byte) (*State, error) {
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	start := strings.Index(text, magic+" ")
	if start < 0 {
		return nil, ErrCorrupt
	}
	text = text[start:]
	end := strings.Index(text, "\n"+magic+" END")
	if end < 0 {
		return nil, ErrCorrupt
	}
	lines := strings.Split(text[:end], "\n")
	var format int
	var sum string
	if _, err := fmt.Sscanf(lines[0], magic+" %d %s", &format, &sum); err != nil {
		return nil, ErrCorrupt
	}
	gz, err := base64.StdEncoding.DecodeString(strings.Join(lines[1:], ""))
	if err != nil {
		return nil, ErrCorrupt
	}
	got := sha256.Sum256(gz)
	if hex.EncodeToString(got[:]) != sum {
		return nil, ErrCorrupt
	}
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, ErrCorrupt
	}
	js, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		return nil, ErrCorrupt
	}
	s := New()
	if err := json.Unmarshal(js, s); err != nil {
		return nil, ErrCorrupt
	}
	if s.Maps == nil {
		s.Maps = map[string]Map{}
	}
	if s.Format > Format || format > Format {
		return s, ErrNewer
	}
	return s, nil
}
