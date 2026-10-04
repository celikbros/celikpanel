//go:build linux

package recoveryruntime

import (
	"bytes"
	"fmt"
)

const mailEnrollmentReloadLimit = 3

type mailEnrollmentReload struct {
	Schema      string `json:"schema"`
	ScopeSHA256 string `json:"scope_sha256"`
	Phase       string `json:"phase"`
	Number      int    `json:"number"`
	Outcome     string `json:"outcome"`
}
type mailEnrollmentReloadBudget struct{ Operation, Phase string }

func (e *mailEnrollmentReloadBudget) Error() string {
	return fmt.Sprintf("mail renewal enrollment %s reached its daemon-reload limit at %s; the server owner must inspect native systemd errors, run sudo systemctl daemon-reload after resolving them, and resume this same operation for verification; retained attempts are not reset", e.Operation, e.Phase)
}
func readMailEnrollmentReloads(c *mailFilesContext, scope mailEnrollmentScope, phase string) (int, error) {
	switch phase {
	case "load-forward", "load-rollback", "enable-forward", "enable-rollback":
	default:
		return 0, fail(ReasonUnsupported)
	}
	raw, _ := promotionJSON(scope)
	attempts := 0
	for n := 1; n <= mailEnrollmentReloadLimit; n++ {
		for _, outcome := range []string{"admitted", "native_command_failed"} {
			suffix := ""
			if outcome == "native_command_failed" {
				suffix = "-failed"
			}
			name := fmt.Sprintf("%s.enrollment-%s-attempt-%d%s.json", scope.Operation, phase, n, suffix)
			got, found, err := c.read(name)
			if err != nil {
				return 0, err
			}
			if !found {
				continue
			}
			want, _ := promotionJSON(mailEnrollmentReload{mailEnrollmentSchema, Digest(raw), phase, n, outcome})
			if !bytes.Equal(got, want) {
				return 0, fail(ReasonChanged)
			}
			if outcome == "admitted" {
				if n != attempts+1 {
					return 0, fail(ReasonChanged)
				}
				attempts = n
			} else if n > attempts {
				return 0, fail(ReasonChanged)
			}
		}
	}
	return attempts, nil
}
func runMailEnrollmentReload(c *mailFilesContext, scope mailEnrollmentScope, phase string, verify func() error, action func() error, checkpoint func(string)) error {
	n, err := readMailEnrollmentReloads(c, scope, phase)
	if err != nil {
		return err
	}
	if n >= mailEnrollmentReloadLimit {
		return &mailEnrollmentReloadBudget{scope.Operation, phase}
	}
	raw, _ := promotionJSON(scope)
	admitted, _ := promotionJSON(mailEnrollmentReload{mailEnrollmentSchema, Digest(raw), phase, n + 1, "admitted"})
	name := fmt.Sprintf("%s.enrollment-%s-attempt-%d", scope.Operation, phase, n+1)
	if err = publishMailRecord(c, name+".json", admitted, verify, checkpoint, "enrollment_"+phase+"_attempt"); err != nil {
		return err
	}
	if err = verify(); err != nil {
		return err
	}
	if err = action(); err != nil {
		failed, _ := promotionJSON(mailEnrollmentReload{mailEnrollmentSchema, Digest(raw), phase, n + 1, "native_command_failed"})
		if e := publishMailRecord(c, name+"-failed.json", failed, verify, checkpoint, "enrollment_"+phase+"_failure"); e != nil {
			return e
		}
		return err
	}
	return verify()
}
