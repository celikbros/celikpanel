package hostplatform

import (
	"errors"
	"strings"
	"testing"
)

// The line this package draws is the whole point of R-048: a host that has not
// finished starting is "not yet" and may be asked again, while a host that
// cannot be managed is "no" and asking again is pointless. Getting that
// backwards either wedges a machine that was only slow to boot, or abandons a
// plan on a machine that was never going to answer.
// R-048'in butun mesele ettigi ayrim: "henuz degil" ile "hayir".

func TestDetectionMarksABootConditionAsRetryable(t *testing.T) {
	probe := readyProbe("pacman", "systemctl")
	probe.SystemdReady = func(string) error {
		return stillStarting(errors.New(`systemd state "starting" exited with status 1`))
	}
	_, err := DetectWith([]byte("ID=arch\n"), probe)
	if err == nil {
		t.Fatal("a host that had not finished starting was accepted as ready")
	}
	if !StillStarting(err) {
		t.Fatalf("boot condition was not retryable: %v", err)
	}
	if Unsupported(err) {
		t.Fatalf("a host that was only still starting was called unsupported: %v", err)
	}
	if !strings.Contains(err.Error(), "systemd is not ready") ||
		!strings.Contains(err.Error(), `systemd state "starting"`) {
		t.Fatalf("classifying the failure changed what it says: %q", err.Error())
	}
}

func TestDetectionMarksEveryOtherRefusalAsDurable(t *testing.T) {
	tests := []struct {
		name    string
		release string
		probe   func() Probe
		want    string
	}{
		{
			name:    "settled systemd state",
			release: "ID=arch\n",
			probe: func() Probe {
				probe := readyProbe("pacman", "systemctl")
				probe.SystemdReady = func(string) error { return errors.New("offline") }
				return probe
			},
			want: "systemd is not ready",
		},
		{
			name:    "no supported package manager",
			release: "ID=arch\n",
			probe:   func() Probe { return readyProbe("systemctl") },
			want:    "requires pacman",
		},
		{
			name:    "unparsable os-release",
			release: "not a key\n",
			probe:   func() Probe { return readyProbe("pacman", "systemctl") },
			want:    "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DetectWith([]byte(test.release), test.probe())
			if err == nil {
				t.Fatal("a refusal was expected")
			}
			if !Unsupported(err) {
				t.Fatalf("a durable refusal was not marked durable: %v", err)
			}
			if StillStarting(err) {
				t.Fatalf("a durable refusal was offered as retryable: %v", err)
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("classifying the failure changed what it says: %q", err.Error())
			}
		})
	}
}

func TestMarkUnsupportedNeverOverridesABootCondition(t *testing.T) {
	booting := stillStarting(errors.New("still starting"))
	if marked := MarkUnsupported(booting); Unsupported(marked) || !StillStarting(marked) {
		t.Fatalf("a boot condition was re-labelled as a durable refusal: %v", marked)
	}
	if MarkUnsupported(nil) != nil {
		t.Fatal("a nil result was turned into a refusal")
	}
	durable := MarkUnsupported(errors.New("unsupported package manager \"\""))
	if !Unsupported(durable) || StillStarting(durable) ||
		durable.Error() != `unsupported package manager ""` {
		t.Fatalf("caller-marked durable refusal = %v", durable)
	}
}
