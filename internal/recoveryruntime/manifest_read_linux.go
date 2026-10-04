//go:build linux

package recoveryruntime

// ManifestBytes reads the already pinned and bounded manifest descriptor. It
// never reopens its path; a replacement FIFO cannot block enrollment or dispatch.
func (runtime *Runtime) ManifestBytes() ([]byte, error) {
	if runtime == nil || runtime.state == nil {
		return nil, fail(ReasonChanged)
	}
	state := runtime.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed || runtime.Root != state.root || runtime.Digest != state.digest {
		return nil, fail(ReasonChanged)
	}
	for _, file := range state.files {
		if file.base == ManifestName && file.parent.path == state.root {
			raw, err := file.readBounded()
			if err != nil {
				return nil, err
			}
			if Digest(raw) != state.digest {
				return nil, fail(ReasonDigestMismatch)
			}
			return raw, nil
		}
	}
	return nil, fail(ReasonInvalidManifest)
}
