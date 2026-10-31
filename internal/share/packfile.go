package share

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Portable local package magic. Distinct from Seal()'s encrypted CMNETSHARE1
// so a friend can exchange a file without a relay code.
var packMagic = []byte("CMNETPACK1\n")

// WritePack writes an unencrypted portable package (gzip+tar of Meta+files).
// Suitable for intentional captain→friend file exchange under local-only mode.
func WritePack(w io.Writer, p Payload) error {
	if p.Meta.SharedAt.IsZero() {
		p.Meta.SharedAt = time.Now().UTC()
	}
	plain, err := pack(p)
	if err != nil {
		return err
	}
	if _, err := w.Write(packMagic); err != nil {
		return err
	}
	_, err = w.Write(plain)
	return err
}

// ReadPack reads a portable package produced by WritePack.
func ReadPack(r io.Reader) (Payload, error) {
	head := make([]byte, len(packMagic))
	if _, err := io.ReadFull(r, head); err != nil {
		return Payload{}, fmt.Errorf("не удалось прочитать пакет: %w", err)
	}
	if !bytes.Equal(head, packMagic) {
		return Payload{}, errors.New("файл не похож на локальный пакет cmnet (ожидался CMNETPACK1)")
	}
	body, err := io.ReadAll(io.LimitReader(r, MaxPayload))
	if err != nil {
		return Payload{}, err
	}
	return unpack(body)
}

// WritePackFile writes a portable package to path (atomic replace).
func WritePackFile(path string, p Payload) error {
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = WritePack(f, p)
	cerr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	return os.Rename(tmp, path)
}

// ReadPackFile reads a portable package from path.
func ReadPackFile(path string) (Payload, error) {
	f, err := os.Open(path)
	if err != nil {
		return Payload{}, err
	}
	defer f.Close()
	return ReadPack(f)
}
