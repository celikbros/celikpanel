package main

import (
	"os"
	"path/filepath"
)

// Runtime paths and address. Defaults keep `./bin/panel` working straight
// from the repo for development; a system install (see install.sh) overrides
// them via environment so nothing is hard-coded to one layout.
//
// Çalışma yolları ve adres. Varsayılanlar `./bin/panel`in depodan doğrudan
// çalışmasını sağlar; sistem kurulumu (bkz. install.sh) bunları ortamla
// geçersiz kılar; böylece hiçbir şey tek bir düzene gömülü kalmaz.

// dataDir is where the SQLite database lives (CELIKPANEL_DATA_DIR).
// dataDir, SQLite veritabanının yaşadığı yerdir (CELIKPANEL_DATA_DIR).
func dataDir() string {
	if d := os.Getenv("CELIKPANEL_DATA_DIR"); d != "" {
		return d
	}
	return "./data"
}

func databaseFile() string {
	return filepath.Join(dataDir(), "celikpanel.db")
}

// webDir is the built frontend to serve (CELIKPANEL_WEB_DIR).
// webDir, sunulacak derlenmiş ön yüzdür (CELIKPANEL_WEB_DIR).
func webDir() string {
	if d := os.Getenv("CELIKPANEL_WEB_DIR"); d != "" {
		return d
	}
	return "./web/dist"
}

// listenAddr is the HTTP bind address (CELIKPANEL_LISTEN, e.g. ":2083" or
// "127.0.0.1:2083"). 2083 is the port hosting users already know from cPanel.
// listenAddr, HTTP bağlanma adresidir (CELIKPANEL_LISTEN). 2083, barındırma
// kullanıcılarının cPanel'den zaten tanıdığı porttur.
func listenAddr() string {
	if a := os.Getenv("CELIKPANEL_LISTEN"); a != "" {
		return a
	}
	return ":2083"
}
