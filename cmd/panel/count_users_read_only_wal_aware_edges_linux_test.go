//go:build linux

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCountUsableUsersReadOnlyWALAwareRejectsInvalidMainHeaderFormats(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		offset int
		value  byte
	}{
		{name: "write-format-zero", offset: 18, value: 0},
		{name: "read-format-three", offset: 19, value: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			path := newClosedAdmissionDatabase(t)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			content[testCase.offset] = testCase.value
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			clear(content)
			if _, err := countUsableUsersReadOnlyWALAware(path); err == nil {
				t.Fatal("database with an invalid main-header format was accepted")
			}
		})
	}
}

func TestCountUsableUsersReadOnlyWALAwareRejectsInvalidCommittedWALHeader(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, source)
	if _, err := database.GetDB().Exec(`PRAGMA user_version = 7`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	requireNonEmptyWAL(t, source)
	path := copyAdmissionDatabaseAndWAL(t, source)
	database.Close()

	walPath := path + "-wal"
	wal, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	layout := parseAdmissionWAL(t, wal)
	lastPageOne := -1
	for index := 0; index < layout.frameCount; index++ {
		frameOffset := 32 + index*layout.frameSize
		if binary.BigEndian.Uint32(wal[frameOffset:frameOffset+4]) == 1 {
			lastPageOne = index
		}
	}
	if lastPageOne < 0 {
		t.Fatal("test WAL has no committed page-one frame")
	}
	rewriteAdmissionWALChecksums(t, wal, func(index int, frame []byte) {
		if index == lastPageOne {
			frame[24+18] = 3
		}
	})
	if err := os.WriteFile(walPath, wal, 0o600); err != nil {
		t.Fatal(err)
	}
	clear(wal)
	if _, err := countUsableUsersReadOnlyWALAware(path); err == nil {
		t.Fatal("checksum-valid WAL with an invalid final SQLite header was accepted")
	}
}

func TestCountUsableUsersReadOnlyWALAwareExcludesChecksumValidUncommittedTail(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, source)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('committed-admin', 'committed-hash', 'committed@example.test', 'admin')
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	path := copyAdmissionDatabaseAndWAL(t, source)
	database.Close()

	mainImage, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	walPath := path + "-wal"
	wal, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	layout, checksum := verifyAdmissionWALChecksums(t, wal)
	if len(mainImage) < layout.pageSize {
		t.Fatal("main image is shorter than one page")
	}
	frame := make([]byte, layout.frameSize)
	binary.BigEndian.PutUint32(frame[0:4], 1)
	copy(frame[8:16], wal[16:24])
	copy(frame[24:], mainImage[:layout.pageSize])
	frame[24+18] = 3
	checksum = sqliteWALChecksum(layout.order, frame[:8], checksum)
	checksum = sqliteWALChecksum(layout.order, frame[24:], checksum)
	binary.BigEndian.PutUint32(frame[16:20], checksum[0])
	binary.BigEndian.PutUint32(frame[20:24], checksum[1])
	wal = append(wal, frame...)
	if err := os.WriteFile(walPath, wal, 0o600); err != nil {
		t.Fatal(err)
	}
	clear(mainImage)
	clear(frame)
	clear(wal)

	count, err := countUsableUsersReadOnlyWALAware(path)
	if err != nil {
		t.Fatalf("valid committed prefix with an uncommitted tail was rejected: %v", err)
	}
	if count != 1 {
		t.Fatalf("usable user count = %d, want committed prefix count 1", count)
	}
}

func TestCountUsableUsersReadOnlyWALAwareAppliesRepeatedPagesAcrossCommits(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, source)
	statements := []string{
		`INSERT INTO users (username, password_hash, email, role) VALUES ('first-admin', 'hash-one', 'first@example.test', 'admin')`,
		`UPDATE users SET email = 'updated@example.test' WHERE username = 'first-admin'`,
		`INSERT INTO users (username, password_hash, email, role) VALUES ('second-admin', 'hash-two', 'second@example.test', 'admin')`,
	}
	for _, statement := range statements {
		if _, err := database.GetDB().Exec(statement); err != nil {
			database.Close()
			t.Fatal(err)
		}
	}
	path := copyAdmissionDatabaseAndWAL(t, source)
	database.Close()

	wal, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := verifyAdmissionWALChecksums(t, wal)
	pageVisits := make(map[uint32]int)
	commits := 0
	for index := 0; index < layout.frameCount; index++ {
		offset := 32 + index*layout.frameSize
		pageVisits[binary.BigEndian.Uint32(wal[offset:offset+4])]++
		if binary.BigEndian.Uint32(wal[offset+4:offset+8]) != 0 {
			commits++
		}
	}
	clear(wal)
	if commits < len(statements) {
		t.Fatalf("test WAL has %d commits, want at least %d", commits, len(statements))
	}
	hasRepeatedPage := false
	for _, visits := range pageVisits {
		if visits > 1 {
			hasRepeatedPage = true
			break
		}
	}
	if !hasRepeatedPage {
		t.Fatal("test WAL did not exercise a repeated page")
	}
	count, err := countUsableUsersReadOnlyWALAware(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("usable user count = %d, want 2", count)
	}
}

func TestCountUsableUsersReadOnlyWALAwareAppliesFinalCommitShrink(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, source)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('shrink-admin', 'shrink-hash', 'shrink@example.test', 'admin')
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`CREATE TABLE admission_bloat (payload BLOB NOT NULL)`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	transaction, err := database.GetDB().Begin()
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	statement, err := transaction.Prepare(`INSERT INTO admission_bloat(payload) VALUES (randomblob(32768))`)
	if err != nil {
		_ = transaction.Rollback()
		database.Close()
		t.Fatal(err)
	}
	for index := 0; index < 96; index++ {
		if _, err := statement.Exec(); err != nil {
			_ = statement.Close()
			_ = transaction.Rollback()
			database.Close()
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		_ = transaction.Rollback()
		database.Close()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	checkpointWAL(t, database.GetDB(), "TRUNCATE")
	mainInfo, err := os.Stat(source)
	if err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`DELETE FROM admission_bloat`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if _, err := database.GetDB().Exec(`VACUUM`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	requireNonEmptyWAL(t, source)
	path := copyAdmissionDatabaseAndWAL(t, source)
	database.Close()

	wal, err := os.ReadFile(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	layout, _ := verifyAdmissionWALChecksums(t, wal)
	lastFrame := 32 + (layout.frameCount-1)*layout.frameSize
	committedPages := binary.BigEndian.Uint32(wal[lastFrame+4 : lastFrame+8])
	clear(wal)
	if committedPages == 0 {
		t.Fatal("final WAL frame is not a commit")
	}
	committedSize := int64(committedPages) * int64(layout.pageSize)
	if committedSize >= mainInfo.Size() {
		t.Fatalf("test did not produce a shrink: final=%d main=%d", committedSize, mainInfo.Size())
	}
	count, err := countUsableUsersReadOnlyWALAware(path)
	if err != nil {
		t.Fatalf("final-commit shrink was rejected: %v", err)
	}
	if count != 1 {
		t.Fatalf("usable user count = %d, want 1 after shrink", count)
	}
}

func TestCountUsableUsersReadOnlyWALAwarePreservesAccessTimes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	database := openWALAwareTestDatabase(t, source)
	if _, err := database.GetDB().Exec(`
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('atime-admin', 'atime-hash', 'atime@example.test', 'admin')
	`); err != nil {
		database.Close()
		t.Fatal(err)
	}
	path := copyAdmissionDatabaseAndWAL(t, source)
	database.Close()

	wantAtime := unix.NsecToTimespec(time.Unix(946684800, 123456789).UnixNano())
	before := make(map[string]unix.Timespec, 2)
	for _, suffix := range []string{"", "-wal"} {
		var state unix.Stat_t
		if err := unix.Stat(path+suffix, &state); err != nil {
			t.Fatal(err)
		}
		if err := unix.UtimesNano(path+suffix, []unix.Timespec{wantAtime, state.Mtim}); err != nil {
			t.Fatal(err)
		}
		if err := unix.Stat(path+suffix, &state); err != nil {
			t.Fatal(err)
		}
		before[suffix] = state.Atim
	}
	if _, err := countUsableUsersReadOnlyWALAware(path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal"} {
		var state unix.Stat_t
		if err := unix.Stat(path+suffix, &state); err != nil {
			t.Fatal(err)
		}
		if state.Atim != before[suffix] {
			t.Fatalf("source %s atime changed: before=%v after=%v", suffix, before[suffix], state.Atim)
		}
	}
}

func TestCountUsableUsersReadOnlyWALAwareRejectsOversizedSparseInputs(t *testing.T) {
	t.Run("main", func(t *testing.T) {
		path := newClosedAdmissionDatabase(t)
		if err := os.Truncate(path, maxReadOnlyAdmissionDatabaseBytes+4096); err != nil {
			t.Fatal(err)
		}
		if _, err := countUsableUsersReadOnlyWALAware(path); err == nil {
			t.Fatal("oversized sparse main database was accepted")
		}
	})
	t.Run("wal", func(t *testing.T) {
		path := newClosedAdmissionDatabase(t)
		if err := os.WriteFile(path+"-wal", make([]byte, 32), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path+"-wal", maxReadOnlyAdmissionDatabaseBytes+4096); err != nil {
			t.Fatal(err)
		}
		if _, err := countUsableUsersReadOnlyWALAware(path); err == nil {
			t.Fatal("oversized sparse WAL was accepted")
		}
	})
}

type admissionWALLayout struct {
	pageSize   int
	frameSize  int
	frameCount int
	order      binary.ByteOrder
}

func parseAdmissionWAL(t *testing.T, wal []byte) admissionWALLayout {
	t.Helper()
	if len(wal) < 32 {
		t.Fatal("WAL is shorter than its header")
	}
	magic := binary.BigEndian.Uint32(wal[0:4])
	var order binary.ByteOrder = binary.LittleEndian
	if magic == 0x377f0683 {
		order = binary.BigEndian
	} else if magic != 0x377f0682 {
		t.Fatalf("invalid WAL magic %#x", magic)
	}
	pageSize := int(binary.BigEndian.Uint32(wal[8:12]))
	if pageSize == 1 {
		pageSize = 65536
	}
	frameSize := 24 + pageSize
	if pageSize < 512 || (len(wal)-32)%frameSize != 0 {
		t.Fatalf("invalid test WAL shape: page=%d bytes=%d", pageSize, len(wal))
	}
	return admissionWALLayout{
		pageSize:   pageSize,
		frameSize:  frameSize,
		frameCount: (len(wal) - 32) / frameSize,
		order:      order,
	}
}

func verifyAdmissionWALChecksums(t *testing.T, wal []byte) (admissionWALLayout, [2]uint32) {
	t.Helper()
	layout := parseAdmissionWAL(t, wal)
	checksum := sqliteWALChecksum(layout.order, wal[:24], [2]uint32{})
	wantHeader := [2]uint32{
		binary.BigEndian.Uint32(wal[24:28]),
		binary.BigEndian.Uint32(wal[28:32]),
	}
	if checksum != wantHeader {
		t.Fatalf("invalid test WAL header checksum: got=%v want=%v", checksum, wantHeader)
	}
	for index := 0; index < layout.frameCount; index++ {
		offset := 32 + index*layout.frameSize
		frame := wal[offset : offset+layout.frameSize]
		if string(frame[8:16]) != string(wal[16:24]) {
			t.Fatalf("test WAL frame %d has stale salt", index+1)
		}
		checksum = sqliteWALChecksum(layout.order, frame[:8], checksum)
		checksum = sqliteWALChecksum(layout.order, frame[24:], checksum)
		want := [2]uint32{
			binary.BigEndian.Uint32(frame[16:20]),
			binary.BigEndian.Uint32(frame[20:24]),
		}
		if checksum != want {
			t.Fatalf("invalid test WAL frame %d checksum: got=%v want=%v", index+1, checksum, want)
		}
	}
	return layout, checksum
}

func rewriteAdmissionWALChecksums(t *testing.T, wal []byte, mutate func(int, []byte)) {
	t.Helper()
	layout, _ := verifyAdmissionWALChecksums(t, wal)
	checksum := sqliteWALChecksum(layout.order, wal[:24], [2]uint32{})
	for index := 0; index < layout.frameCount; index++ {
		offset := 32 + index*layout.frameSize
		frame := wal[offset : offset+layout.frameSize]
		mutate(index, frame)
		checksum = sqliteWALChecksum(layout.order, frame[:8], checksum)
		checksum = sqliteWALChecksum(layout.order, frame[24:], checksum)
		binary.BigEndian.PutUint32(frame[16:20], checksum[0])
		binary.BigEndian.PutUint32(frame[20:24], checksum[1])
	}
}

func newClosedAdmissionDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.sqlite")
	database := openWALAwareTestDatabase(t, path)
	database.Close()
	return path
}

func copyAdmissionDatabaseAndWAL(t *testing.T, source string) string {
	t.Helper()
	destination := filepath.Join(t.TempDir(), "panel.sqlite")
	for _, suffix := range []string{"", "-wal"} {
		content, err := os.ReadFile(source + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination+suffix, content, 0o600); err != nil {
			clear(content)
			t.Fatal(err)
		}
		clear(content)
	}
	return destination
}

func (layout admissionWALLayout) String() string {
	return fmt.Sprintf("page=%d frame=%d count=%d", layout.pageSize, layout.frameSize, layout.frameCount)
}
