package main

import (
	"errors"
	"strings"
)

// The pacman bind package ships its single /etc/named.conf with two option
// directives that CelikPanel's managed block owns on every layout:
//
//	allow-recursion { 127.0.0.1; ::1; };
//	allow-transfer { none; };
//
// On APT those directives are absent from the stock named.conf.options, so the
// managed block is simply inserted. On pacman the same insertion refused with
// "BIND options already define allow-recursion outside CelikPanel ownership"
// before the first switch could begin (live on Arch, 3 September 2026;
// register R-018). The managed block is strictly tighter than both stock
// values (no recursion at all, transfers only to a paired primary), so the
// stock lines are superseded, not overridden: exactly those two lines, with
// exactly those values, are dropped before the block is added. Any other
// value is an operator's decision and still refuses.
//
// pacman bind paketi tek /etc/named.conf'unu, CelikPanel'in yönetilen bloğunun
// her yerleşimde sahip olduğu iki seçenek direktifiyle gönderir:
//
//	allow-recursion { 127.0.0.1; ::1; };
//	allow-transfer { none; };
//
// APT'de bu direktifler stok named.conf.options'ta yoktur; yönetilen blok
// doğrudan eklenir. pacman'da aynı ekleme, ilk geçiş başlayamadan "BIND
// options already define allow-recursion outside CelikPanel ownership" ile
// reddediyordu (3 Eylül 2026'da Arch'ta canlı; defter R-018). Yönetilen blok
// iki stok değerden de kesinlikle daha sıkıdır (hiç özyineleme yok, aktarım
// yalnız eşleşmiş birincile), dolayısıyla stok satırlar geçersiz kılınmaz,
// üstlenilir: tam o iki satır, tam o değerlerle, blok eklenmeden önce
// düşürülür. Başka her değer operatörün kararıdır ve reddedilmeye devam eder.
var pacmanStockBINDOptionDirectives = []string{
	"allow-recursion { 127.0.0.1; ::1; };",
	"allow-transfer { none; };",
}

// stripStockPacmanBINDOptionDirectives removes the stock pacman directives
// from the options block when they appear as whole lines with exactly the
// shipped values. Lines outside the options block, commented lines and any
// other value are left untouched for managedBINDOptions to judge.
// stripStockPacmanBINDOptionDirectives, stok pacman direktiflerini seçenekler
// bloğunda tam satır ve tam gönderilen değerlerle göründüklerinde kaldırır.
// Blok dışındaki satırlar, yorumlanmış satırlar ve başka her değer,
// managedBINDOptions yargılasın diye dokunulmadan bırakılır.
func stripStockPacmanBINDOptionDirectives(config string) (string, error) {
	open, close, err := bindOptionsBlock(config)
	if err != nil {
		return "", err
	}
	if open < 0 || close <= open || close > len(config) {
		return "", errors.New("BIND options block is invalid")
	}
	var out strings.Builder
	out.Grow(len(config))
	offset := 0
	for offset < len(config) {
		end := strings.IndexByte(config[offset:], '\n')
		lineEnd := len(config)
		if end >= 0 {
			lineEnd = offset + end + 1
		}
		line := config[offset:lineEnd]
		insideOptions := offset > open && lineEnd-1 <= close
		if !(insideOptions && isStockPacmanBINDOptionDirective(line)) {
			out.WriteString(line)
		}
		offset = lineEnd
	}
	return out.String(), nil
}

func isStockPacmanBINDOptionDirective(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, directive := range pacmanStockBINDOptionDirectives {
		if trimmed == directive {
			return true
		}
	}
	return false
}
