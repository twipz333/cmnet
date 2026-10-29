// Package share hands the host's model and scouting to a guest over the
// internet, with no server of our own and no ports to open.
//
// The host shows a short code; the guest types it in. Both sides derive from the
// code (a) where to meet on a public relay and (b) the key the payload is
// encrypted with, so the relay only ever forwards ciphertext and nobody without
// the code can read or forge it.
//
// Two relays are tried at once and whichever connects first wins:
//   - the magic-wormhole transit relay (a plain TCP pipe, port 4001), and
//   - a piping server over HTTPS (ppng.io), for networks that block odd ports.
//
// The host keeps an offer open and builds the payload afresh for every guest
// that comes, so the same code also serves the scouting refreshed between
// Battle Cup rounds.
package share

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"time"
)

// File is one file of the payload, by its path relative to the data folder.
type File struct {
	Name string
	Data []byte
}

// Meta describes what was shared, for the guest's screen.
type Meta struct {
	BundleVersion string    `json:"bundleVersion"`
	PatchTag      string    `json:"patchTag"`
	HostVersion   string    `json:"hostVersion"`
	SharedAt      time.Time `json:"sharedAt"`
	HasSnapshot   bool      `json:"hasSnapshot"`
	Scouted       int       `json:"scouted"` // enemy players in the snapshot
}

// Payload is what a guest receives.
type Payload struct {
	Meta  Meta
	Files []File
}

// File returns the named file, or nil.
func (p Payload) File(name string) []byte {
	for _, f := range p.Files {
		if f.Name == name {
			return f.Data
		}
	}
	return nil
}

// MaxPayload bounds what a guest accepts. A bundle with portraits is a few tens
// of megabytes; anything near this is not ours.
const MaxPayload = 400 << 20

// --- codes ------------------------------------------------------------------

// Crockford base32: no I, L, O, U, so a code read aloud or retyped survives.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const codeLen = 12 // 60 bits

// NewCode returns a fresh code like "7K3M-QX9D-24HB".
func NewCode() string {
	var b [codeLen]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	var sb strings.Builder
	for i, v := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(v)%len(alphabet)])
	}
	return sb.String()
}

// Normalize turns whatever the guest typed into the canonical code: spaces and
// dashes dropped, case folded, look-alike letters mapped to digits.
func Normalize(code string) (string, error) {
	var sb strings.Builder
	for _, r := range strings.ToUpper(code) {
		switch r {
		case ' ', '-', '_', '\t', '\n', '\r':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(alphabet, r) {
			return "", fmt.Errorf("в коде есть лишний символ %q", r)
		}
		sb.WriteRune(r)
	}
	s := sb.String()
	if len(s) != codeLen {
		return "", fmt.Errorf("код должен быть из %d символов, а в этом %d", codeLen, len(s))
	}
	return s, nil
}

// Format groups a normalized code for display.
func Format(code string) string {
	if n, err := Normalize(code); err == nil {
		code = n
	}
	var sb strings.Builder
	for i, r := range code {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// rendezvous is where both sides meet for a given code and purpose. It is a hash
// of the code, never the code itself, and it differs from the key.
func rendezvous(code, purpose string) [32]byte {
	return sha256.Sum256([]byte("cmnet share v1 " + purpose + ":" + code))
}

// --- sealing ----------------------------------------------------------------

var magic = []byte("CMNETSHARE1\n")

const kdfIterations = 200_000

func key(code string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, code, salt, kdfIterations, 32)
}

// Seal packs and encrypts a payload under code.
func Seal(code string, p Payload) ([]byte, error) {
	code, err := Normalize(code)
	if err != nil {
		return nil, err
	}
	plain, err := pack(p)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	k, err := key(code, salt)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(k)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(magic)+len(salt)+len(nonce)+len(plain)+aead.Overhead())
	out = append(out, magic...)
	out = append(out, salt...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plain, magic), nil
}

// ErrWrongCode means the payload did not decrypt: the code is not the one it
// was sealed with.
var ErrWrongCode = errors.New("код не подходит")

// Open decrypts and unpacks a sealed payload.
func Open(code string, blob []byte) (Payload, error) {
	code, err := Normalize(code)
	if err != nil {
		return Payload{}, err
	}
	head := len(magic) + 16 + 12
	if len(blob) < head || !bytes.Equal(blob[:len(magic)], magic) {
		return Payload{}, errors.New("пришло что-то не от cmnet")
	}
	salt, nonce := blob[len(magic):len(magic)+16], blob[len(magic)+16:head]
	k, err := key(code, salt)
	if err != nil {
		return Payload{}, err
	}
	aead, err := newAEAD(k)
	if err != nil {
		return Payload{}, err
	}
	plain, err := aead.Open(nil, nonce, blob[head:], magic)
	if err != nil {
		return Payload{}, ErrWrongCode
	}
	return unpack(plain)
}

func newAEAD(k []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

const metaName = ".cmnet-share.json"

func pack(p Payload) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	meta, err := json.Marshal(p.Meta)
	if err != nil {
		return nil, err
	}
	files := append([]File{{Name: metaName, Data: meta}}, p.Files...)
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.Name, Mode: 0o644, Size: int64(len(f.Data)), ModTime: p.Meta.SharedAt}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func unpack(b []byte) (Payload, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return Payload{}, err
	}
	tr := tar.NewReader(io.LimitReader(gz, MaxPayload))
	var p Payload
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Payload{}, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		name, ok := SafeName(h.Name)
		if !ok {
			continue // never write outside the data folder
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return Payload{}, err
		}
		if name == metaName {
			if err := json.Unmarshal(data, &p.Meta); err != nil {
				return Payload{}, err
			}
			continue
		}
		p.Files = append(p.Files, File{Name: name, Data: data})
	}
	sort.SliceStable(p.Files, func(i, j int) bool { return p.Files[i].Name < p.Files[j].Name })
	return p, nil
}

// SafeName cleans a payload path and refuses anything that would escape the
// data folder.
func SafeName(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean("/" + name)[1:]
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") {
		return "", false
	}
	return clean, true
}
