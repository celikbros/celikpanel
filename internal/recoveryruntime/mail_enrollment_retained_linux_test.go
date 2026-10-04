//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedMailEnrollmentHelperPinsSourceAndExecutable(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"valid", "missing", "wrong-image", "template-edit", "binary-replaced", "closed", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, _, _ := enrollmentFixture(t, "absent")
			path := filepath.Join(root, "runtime", target, mailrenewalkit.BinaryName)
			if scenario == "missing" {
				os.Remove(path)
			}
			if scenario == "symlink" {
				os.Rename(path, path+".saved")
				os.Symlink(path+".saved", path)
			}
			proof, err := InspectRetainedMailEnrollmentHelper(filepath.Join(root, "runtime"), target)
			if scenario == "missing" || scenario == "symlink" {
				if err == nil {
					proof.Close()
					t.Fatal("unsafe bundle accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer proof.Close()
			executable, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer executable.Close()
			switch scenario {
			case "wrong-image":
				executable.Close()
				executable, err = os.Open("/proc/self/exe")
				if err != nil {
					t.Fatal(err)
				}
				defer executable.Close()
			case "template-edit":
				capturePut(t, filepath.Join(filepath.Dir(path), mailrenewalkit.TimerName), []byte("owner timer"), 0644)
			case "binary-replaced":
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				os.Rename(path, path+".old")
				capturePut(t, path, raw, 0755)
			case "closed":
				proof.Close()
			}
			if err = proof.VerifyExecutable(executable); (err == nil) != (scenario == "valid") {
				t.Fatalf("%s: %v", scenario, err)
			}
		})
	}
}
