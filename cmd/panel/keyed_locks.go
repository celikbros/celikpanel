package main

import "sync"

// keyedLocks serialises work per integer key and forgets a key nobody holds.
// keyedLocks, işi tamsayı anahtar başına sıraya koyar ve kimsenin tutmadığı
// anahtarı unutur.
type keyedLocks struct {
	mu      sync.Mutex
	entries map[int]*keyedLock
}

type keyedLock struct {
	mu   sync.Mutex
	refs int
}

// lock waits for the key and returns its release.
func (k *keyedLocks) lock(key int) func() {
	k.mu.Lock()
	if k.entries == nil {
		k.entries = make(map[int]*keyedLock)
	}
	entry := k.entries[key]
	if entry == nil {
		entry = &keyedLock{}
		k.entries[key] = entry
	}
	entry.refs++
	k.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.entries, key)
		}
		k.mu.Unlock()
	}
}

// databaseAdminAccountLocks: one change of the Panel's own account per
// database server at a time (D-029).
// databaseAdminAccountLocks: veritabanı sunucusu başına Panel'in kendi
// hesabında aynı anda tek değişiklik (D-029).
var databaseAdminAccountLocks keyedLocks
