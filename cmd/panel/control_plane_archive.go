package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The control-plane archive is a new, versioned, self-describing container. It
// is not the domain-backup format and it deliberately does not reuse the agent
// backup manifest: a control-plane archive restores a host identity, so its own
// bytes must say which schema, which build and which layout produced them.
//
// File layout:
//
//	"CPBAK1\n" | uint32 big-endian header length | header JSON | ciphertext
//
// The header travels in the clear because a fresh host must be able to read the
// key-derivation parameters before it has anything else. Every byte before the
// ciphertext is the AAD of every chunk, so the header cannot be edited without
// breaking authentication.
//
// Kontrol düzlemi arşivi yeni, sürümlenmiş ve kendini tanımlayan bir kaptır.
// Alan yedeği biçimi değildir ve agent yedek manifestini bilerek yeniden
// kullanmaz: bir kontrol düzlemi arşivi makine kimliğini geri yükler, bu yüzden
// hangi şema, hangi yapı ve hangi yerleşimin ürettiğini kendi baytları söyler.

const (
	controlPlaneArchiveMagic           = "CPBAK1\n"
	controlPlaneArchiveFormat          = 1
	controlPlaneArchiveKDF             = "argon2id"
	controlPlaneArchiveArgonTime       = 3
	controlPlaneArchiveArgonMemoryKiB  = 64 * 1024
	controlPlaneArchiveArgonThreads    = 4
	controlPlaneArchiveSaltBytes       = 16
	controlPlaneArchiveChunkBytes      = 64 * 1024
	controlPlaneArchivePayloadKeyBytes = 32
	// controlPlaneArchiveMaxHeaderBytes bounds a hostile header before any
	// allocation is made for it.
	controlPlaneArchiveMaxHeaderBytes = 64 * 1024
	controlPlaneManifestName          = "manifest.json"
	controlPlaneManifestDigestName    = "manifest.sha256"
)

var errControlPlaneArchiveAuthentication = errors.New(
	"control-plane archive did not authenticate: the backup key is wrong or the archive was altered",
)

type controlPlaneArchiveHeader struct {
	Format       int    `json:"format"`
	KDF          string `json:"kdf"`
	Time         uint32 `json:"time"`
	MemoryKiB    uint32 `json:"memory_kib"`
	Threads      uint8  `json:"threads"`
	Salt         []byte `json:"salt"`
	Chunk        int    `json:"chunk"`
	CreatedAt    string `json:"created_at"`
	PanelVersion string `json:"panel_version"`
	PanelCommit  string `json:"panel_commit"`
}

// controlPlaneManifestEntry describes one archived path exactly as it was found
// on the live host. Owner and group are recorded as NAMES, because a uid number
// is a property of the dead host while a name is the property the operator and
// the distribution packages agree on.
type controlPlaneManifestEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Owner  string `json:"owner"`
	Group  string `json:"group"`
	Mode   string `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

const (
	controlPlaneManifestEntryFile      = "file"
	controlPlaneManifestEntryDirectory = "dir"
)

type controlPlaneManifest struct {
	SchemaVersion int `json:"schema_version"`
	// DatabaseMigrationVersion is the archived database own migration high
	// water mark, read from the staged copy at creation time. SchemaVersion
	// says which durable service-operation contract the WRITING BINARY was
	// built against; this says how far the DATA has been migrated. They answer
	// different questions and a restore has to ask both: a database migrated
	// past what the restoring release ships must be refused before it is
	// placed, or an older panel would open a schema it cannot run.
	// DatabaseMigrationVersion, arşivlenen veritabanının kendi migration
	// seviyesidir. SchemaVersion yazan ikilinin sözleşmesini söyler; bu ise
	// verinin ne kadar ilerlediğini. Geri yükleme ikisini de sormak zorundadır.
	DatabaseMigrationVersion int    `json:"database_migration_version"`
	PanelVersion             string `json:"panel_version"`
	PanelCommit              string `json:"panel_commit"`
	Host                     string `json:"host"`
	CreatedAt                string `json:"created_at"`
	// Roots records the six directories the inventory was read from. It is the
	// one field beyond docs/DISASTER-RECOVERY.md section 4: without it a
	// restore has to guess how a recorded absolute path maps onto this host,
	// and the panel already lets an installation move every one of these roots
	// by environment. With it, restoring onto a differently laid out host is an
	// ordinary, testable operation instead of a special case.
	Roots   controlPlaneRoots           `json:"roots"`
	Members []controlPlaneManifestEntry `json:"members"`
}

func newControlPlaneArchiveHeader(createdAt string) (controlPlaneArchiveHeader, error) {
	salt := make([]byte, controlPlaneArchiveSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return controlPlaneArchiveHeader{}, fmt.Errorf("generate archive salt: %w", err)
	}
	return controlPlaneArchiveHeader{
		Format:       controlPlaneArchiveFormat,
		KDF:          controlPlaneArchiveKDF,
		Time:         controlPlaneArchiveArgonTime,
		MemoryKiB:    controlPlaneArchiveArgonMemoryKiB,
		Threads:      controlPlaneArchiveArgonThreads,
		Salt:         salt,
		Chunk:        controlPlaneArchiveChunkBytes,
		CreatedAt:    createdAt,
		PanelVersion: buildVersion,
		PanelCommit:  buildCommit,
	}, nil
}

func (h controlPlaneArchiveHeader) validate() error {
	if h.Format != controlPlaneArchiveFormat {
		return fmt.Errorf("unsupported control-plane archive format %d", h.Format)
	}
	if h.KDF != controlPlaneArchiveKDF {
		return fmt.Errorf("unsupported control-plane archive key derivation %q", h.KDF)
	}
	if h.Time < 1 || h.Time > 16 {
		return fmt.Errorf("control-plane archive time cost %d is out of range", h.Time)
	}
	if h.MemoryKiB < 8*1024 || h.MemoryKiB > 1024*1024 {
		return fmt.Errorf("control-plane archive memory cost %d KiB is out of range", h.MemoryKiB)
	}
	if h.Threads < 1 || h.Threads > 16 {
		return fmt.Errorf("control-plane archive thread count %d is out of range", h.Threads)
	}
	if len(h.Salt) != controlPlaneArchiveSaltBytes {
		return fmt.Errorf("control-plane archive salt must be %d bytes", controlPlaneArchiveSaltBytes)
	}
	if h.Chunk < 1024 || h.Chunk > 4*1024*1024 {
		return fmt.Errorf("control-plane archive chunk size %d is out of range", h.Chunk)
	}
	return nil
}

// encodeControlPlaneArchivePreamble returns the exact bytes that precede the
// ciphertext. Those same bytes are the additional authenticated data of every
// chunk.
func encodeControlPlaneArchivePreamble(header controlPlaneArchiveHeader) ([]byte, error) {
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("encode control-plane archive header: %w", err)
	}
	if len(headerJSON) > controlPlaneArchiveMaxHeaderBytes {
		return nil, errors.New("control-plane archive header is too large")
	}
	preamble := make([]byte, 0, len(controlPlaneArchiveMagic)+4+len(headerJSON))
	preamble = append(preamble, controlPlaneArchiveMagic...)
	preamble = binary.BigEndian.AppendUint32(preamble, uint32(len(headerJSON)))
	preamble = append(preamble, headerJSON...)
	return preamble, nil
}

func readControlPlaneArchivePreamble(
	source io.Reader,
) (controlPlaneArchiveHeader, []byte, error) {
	magic := make([]byte, len(controlPlaneArchiveMagic))
	if _, err := io.ReadFull(source, magic); err != nil {
		return controlPlaneArchiveHeader{}, nil, fmt.Errorf("read control-plane archive magic: %w", err)
	}
	if string(magic) != controlPlaneArchiveMagic {
		return controlPlaneArchiveHeader{}, nil, errors.New("this file is not a control-plane archive")
	}
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(source, lengthBytes); err != nil {
		return controlPlaneArchiveHeader{}, nil, fmt.Errorf("read control-plane archive header length: %w", err)
	}
	headerLength := binary.BigEndian.Uint32(lengthBytes)
	if headerLength == 0 || headerLength > controlPlaneArchiveMaxHeaderBytes {
		return controlPlaneArchiveHeader{}, nil, errors.New("control-plane archive header length is out of range")
	}
	headerJSON := make([]byte, headerLength)
	if _, err := io.ReadFull(source, headerJSON); err != nil {
		return controlPlaneArchiveHeader{}, nil, fmt.Errorf("read control-plane archive header: %w", err)
	}
	var header controlPlaneArchiveHeader
	decoder := json.NewDecoder(strings.NewReader(string(headerJSON)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&header); err != nil {
		return controlPlaneArchiveHeader{}, nil, fmt.Errorf("decode control-plane archive header: %w", err)
	}
	if err := header.validate(); err != nil {
		return controlPlaneArchiveHeader{}, nil, err
	}
	preamble := make([]byte, 0, len(magic)+4+len(headerJSON))
	preamble = append(preamble, magic...)
	preamble = append(preamble, lengthBytes...)
	preamble = append(preamble, headerJSON...)
	return header, preamble, nil
}

// newControlPlaneArchiveAEAD derives the payload key from the operator key and
// wipes the derived bytes as soon as AES has taken its own copy.
func newControlPlaneArchiveAEAD(
	key []byte,
	header controlPlaneArchiveHeader,
) (cipher.AEAD, error) {
	payloadKey := argon2.IDKey(
		key,
		header.Salt,
		header.Time,
		header.MemoryKiB,
		header.Threads,
		controlPlaneArchivePayloadKeyBytes,
	)
	defer zeroControlPlaneKey(payloadKey)
	block, err := aes.NewCipher(payloadKey)
	if err != nil {
		return nil, fmt.Errorf("prepare control-plane archive cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("prepare control-plane archive cipher: %w", err)
	}
	return aead, nil
}

// controlPlaneStreamNonce is the age STREAM nonce: an 11-byte big-endian
// counter followed by one byte that is 0x01 only on the final chunk. Truncating
// the ciphertext therefore fails authentication instead of yielding a shorter
// but still valid archive.
func controlPlaneStreamNonce(counter uint64, last bool) ([]byte, error) {
	if counter > 1<<48 {
		return nil, errors.New("control-plane archive is too large for its nonce space")
	}
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[3:11], counter)
	if last {
		nonce[11] = 0x01
	}
	return nonce, nil
}

type controlPlaneStreamWriter struct {
	destination io.Writer
	aead        cipher.AEAD
	aad         []byte
	chunk       int
	buffer      []byte
	counter     uint64
	closed      bool
}

func newControlPlaneStreamWriter(
	destination io.Writer,
	aead cipher.AEAD,
	aad []byte,
	chunk int,
) *controlPlaneStreamWriter {
	return &controlPlaneStreamWriter{
		destination: destination,
		aead:        aead,
		aad:         aad,
		chunk:       chunk,
		buffer:      make([]byte, 0, chunk*2),
	}
}

func (w *controlPlaneStreamWriter) Write(data []byte) (int, error) {
	if w.closed {
		return 0, errors.New("control-plane archive stream is closed")
	}
	w.buffer = append(w.buffer, data...)
	// Flush only on a strict overflow. A plaintext whose length is an exact
	// multiple of the chunk size therefore ends with a FULL final chunk, so an
	// empty final chunk can only ever mean an empty plaintext.
	for len(w.buffer) > w.chunk {
		if err := w.seal(w.buffer[:w.chunk], false); err != nil {
			return 0, err
		}
		w.buffer = append(w.buffer[:0], w.buffer[w.chunk:]...)
	}
	return len(data), nil
}

func (w *controlPlaneStreamWriter) seal(plaintext []byte, last bool) error {
	nonce, err := controlPlaneStreamNonce(w.counter, last)
	if err != nil {
		return err
	}
	sealed := w.aead.Seal(nil, nonce, plaintext, w.aad)
	if _, err := w.destination.Write(sealed); err != nil {
		return fmt.Errorf("write control-plane archive chunk: %w", err)
	}
	w.counter++
	return nil
}

func (w *controlPlaneStreamWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.seal(w.buffer, true)
}

type controlPlaneStreamReader struct {
	source    io.Reader
	aead      cipher.AEAD
	aad       []byte
	chunk     int
	counter   uint64
	pending   []byte
	finished  bool
	peeked    []byte
	peekedEOF bool
}

func newControlPlaneStreamReader(
	source io.Reader,
	aead cipher.AEAD,
	aad []byte,
	chunk int,
) *controlPlaneStreamReader {
	return &controlPlaneStreamReader{source: source, aead: aead, aad: aad, chunk: chunk}
}

func (r *controlPlaneStreamReader) Read(out []byte) (int, error) {
	for len(r.pending) == 0 {
		if r.finished {
			return 0, io.EOF
		}
		if err := r.fill(); err != nil {
			return 0, err
		}
	}
	copied := copy(out, r.pending)
	r.pending = r.pending[copied:]
	return copied, nil
}

// fill decrypts exactly one chunk. Nothing reaches the caller before the chunk
// that produced it has authenticated, and the stream refuses to end without a
// chunk that was sealed as the final one.
func (r *controlPlaneStreamReader) fill() error {
	sealedSize := r.chunk + r.aead.Overhead()
	sealed := make([]byte, sealedSize)
	read, err := r.readSealed(sealed)
	if err != nil {
		return err
	}
	if read < r.aead.Overhead() {
		return errControlPlaneArchiveAuthentication
	}
	last := read < sealedSize || r.peekedEOF
	nonce, err := controlPlaneStreamNonce(r.counter, last)
	if err != nil {
		return err
	}
	plaintext, err := r.aead.Open(nil, nonce, sealed[:read], r.aad)
	if err != nil {
		return errControlPlaneArchiveAuthentication
	}
	if !last && len(plaintext) != r.chunk {
		return errControlPlaneArchiveAuthentication
	}
	if last && len(plaintext) == 0 && r.counter != 0 {
		return errControlPlaneArchiveAuthentication
	}
	r.counter++
	r.pending = plaintext
	r.finished = last
	return nil
}

// readSealed reads one sealed chunk and looks one byte ahead, so a chunk that
// exactly fills the buffer can still be recognised as the final one.
func (r *controlPlaneStreamReader) readSealed(sealed []byte) (int, error) {
	read := copy(sealed, r.peeked)
	r.peeked = nil
	got, err := io.ReadFull(r.source, sealed[read:])
	read += got
	switch {
	case err == nil:
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		r.peekedEOF = true
		return read, nil
	default:
		return 0, fmt.Errorf("read control-plane archive chunk: %w", err)
	}
	lookahead := make([]byte, 1)
	switch _, err := io.ReadFull(r.source, lookahead); {
	case err == nil:
		r.peeked = lookahead
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		r.peekedEOF = true
	default:
		return 0, fmt.Errorf("read control-plane archive chunk: %w", err)
	}
	return read, nil
}

// controlPlaneMemberName turns an absolute host path into the archive member
// name: the path without its volume name and without its leading separator.
// Names are validated on the way in and again on the way out, so an archive can
// never talk a restore into writing outside the tree it was told to write into.
func controlPlaneMemberName(absolutePath string) (string, error) {
	cleaned := filepath.Clean(absolutePath)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("control-plane member path %s must be absolute", absolutePath)
	}
	name := filepath.ToSlash(strings.TrimPrefix(cleaned, filepath.VolumeName(cleaned)))
	name = strings.TrimPrefix(name, "/")
	if err := validateControlPlaneMemberName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validateControlPlaneMemberName(name string) error {
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return errors.New("control-plane member name is empty")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return fmt.Errorf("control-plane member name %q must not be absolute", name)
	}
	if strings.Contains(name, `\`) {
		return fmt.Errorf("control-plane member name %q must not contain a backslash", name)
	}
	for _, component := range strings.Split(trimmed, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("control-plane member name %q has an unsafe component", name)
		}
	}
	return nil
}
