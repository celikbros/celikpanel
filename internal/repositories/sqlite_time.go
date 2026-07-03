package repositories

import (
	"fmt"
	"time"
)

// SQLite stores our timestamps as TEXT (datetime('now')), and the
// modernc.org/sqlite driver hands them back as strings — which the standard
// scanner refuses to store into a time.Time. scanTime wraps a *time.Time so
// these columns scan cleanly across every repository.
//
// SQLite zaman damgalarımızı TEXT olarak saklar (datetime('now')) ve
// modernc.org/sqlite sürücüsü bunları string olarak döndürür; standart
// tarayıcı bunu time.Time'a koymayı reddeder. scanTime bir *time.Time'ı
// sararak bu sütunların her repository'de sorunsuz taranmasını sağlar.
type sqliteTime struct{ dest *time.Time }

func scanTime(dest *time.Time) *sqliteTime { return &sqliteTime{dest} }

func (s *sqliteTime) Scan(value any) error {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		*s.dest = v
	case []byte:
		s.parse(string(v))
	case string:
		s.parse(v)
	default:
		return fmt.Errorf("cannot scan %T into time.Time", value)
	}
	return nil
}

func (s *sqliteTime) parse(str string) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, str); err == nil {
			*s.dest = t
			return
		}
	}
	// Best-effort: an unparseable timestamp leaves the zero value rather
	// than failing the whole row.
	// En iyi çaba: çözülemeyen bir zaman damgası, tüm satırı bozmak yerine
	// sıfır değeri bırakır.
}
