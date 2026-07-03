package fs

import (
	"log"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watcher wraps fsnotify to provide a simplified interface for the Agent
type Watcher struct {
	internal *fsnotify.Watcher
	Events   chan string // Emits file paths that changed
	Errors   chan error
	done     chan bool
}

// NewWatcher creates a new file system watcher
func NewWatcher() (*Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	return &Watcher{
		internal: w,
		Events:   make(chan string),
		Errors:   make(chan error),
		done:     make(chan bool),
	}, nil
}

// Add adds a file or directory to the watcher
func (w *Watcher) Add(path string) error {
	// Resolve absolute path just in case
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	log.Printf("Watcher: Adding path %s", absPath)
	return w.internal.Add(absPath)
}

// Start begins listening for events. It blocks until Close() is called or an error occurs.
// Ideally run this in a goroutine.
func (w *Watcher) Start() {
	defer close(w.Events)
	defer close(w.Errors)

	for {
		select {
		case event, ok := <-w.internal.Events:
			if !ok {
				return
			}
			// We care about Write, Create, Remove, Rename, Chmod
			// For now, let's just emit the name for any change
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				log.Printf("Watcher: Event %s on %s", event.Op, event.Name)
				w.Events <- event.Name
			}
		case err, ok := <-w.internal.Errors:
			if !ok {
				return
			}
			w.Errors <- err
		case <-w.done:
			return
		}
	}
}

// Close stops the watcher
func (w *Watcher) Close() error {
	w.done <- true
	return w.internal.Close()
}
