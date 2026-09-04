package hostplatform

import "errors"

// The difference between "not yet" and "no".
//
// A host-profile probe can fail for two unrelated reasons, and a caller that is
// allowed to wait must be able to tell them apart. ErrStillStarting means the
// machine has not finished booting: the same probe, run later on the same host,
// is expected to succeed, so retrying is the correct response. ErrUnsupported
// means this host cannot be managed at all - no supported package manager, a
// missing or tampered fixed executable, a security policy the family forbids -
// and no amount of waiting changes that, so the only correct response is to
// fail and say why.
//
// Everything a caller may retry carries ErrStillStarting; everything the Linux
// detector refuses for a reason that is a property of the host carries
// ErrUnsupported. An error carrying neither is not a verdict this package
// reached - the non-Linux Detect stub is the example - and callers must treat
// it as neither retryable nor a proven durable refusal.
//
// "Henuz degil" ile "hayir" arasindaki fark. ErrStillStarting, makinenin
// acilisini bitirmedigini soyler ve yeniden denenebilir; ErrUnsupported, bu
// makinenin yonetilemeyecegini soyler ve beklemek bunu degistirmez.
var (
	ErrStillStarting = errors.New("host has not finished starting")
	ErrUnsupported   = errors.New("host platform is unsupported")
)

// StillStarting reports whether a detection error is the transient boot
// condition, which a bounded retry may wait out.
func StillStarting(err error) bool { return errors.Is(err, ErrStillStarting) }

// Unsupported reports whether a detection error is a durable property of this
// host, which no retry can clear.
func Unsupported(err error) bool { return errors.Is(err, ErrUnsupported) }

// MarkUnsupported lets a caller that refuses a host for a reason of its own -
// a package manager this build does not implement, say - record that refusal as
// durable, so a bounded retry does not wait out a verdict that will not change.
// MarkUnsupported, cagiranin kendi kalici retini isaretlemesini saglar.
func MarkUnsupported(err error) error { return unsupported(err) }

// stillStarting tags a probe failure as the boot condition without changing the
// message the operator reads.
func stillStarting(err error) error {
	if err == nil {
		return nil
	}
	return taggedDetectionError{tag: ErrStillStarting, err: err}
}

// unsupported tags a detection refusal as durable without changing its message.
// A failure that is already tagged as the boot condition is never re-tagged:
// "not yet" must never be reported as "no".
func unsupported(err error) error {
	if err == nil || errors.Is(err, ErrStillStarting) {
		return err
	}
	return taggedDetectionError{tag: ErrUnsupported, err: err}
}

// taggedDetectionError carries a classification beside the cause. It keeps the
// cause's text verbatim so every existing message, log line and test that reads
// a detection failure reads exactly what it read before; only errors.Is sees
// the extra sentinel.
type taggedDetectionError struct {
	tag error
	err error
}

func (e taggedDetectionError) Error() string   { return e.err.Error() }
func (e taggedDetectionError) Unwrap() []error { return []error{e.tag, e.err} }
