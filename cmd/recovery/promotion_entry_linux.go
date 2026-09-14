//go:build linux

package main

import "github.com/alicelik/celikpanel/internal/recoveryruntime"

func prepareRuntime(source, mode string) error {
	return prepareRuntimeWith(source, mode, runtimePreparationDependencies{
		boundary: func() error { return recoveryruntime.VerifyPreflightBoundary(9) },
		selected: func() error {
			runtime, err := recoveryruntime.Resolve()
			if err != nil {
				return err
			}
			defer runtime.Close()
			return runtime.Revalidate()
		},
		enroll: func(source string) error { return recoveryruntime.Enroll(source, 9) },
		promote: func(source, mode string) error {
			return recoveryruntime.Promote(recoveryruntime.PromotionRequest{Source: source, Mode: mode}, 9)
		},
	})
}

func dispatchLauncher(args []string) error {
	return dispatchLauncherWith(args, launcherDependencies{
		isEntry:  recoveryruntime.IsLauncherEntry,
		pending:  recoveryruntime.PromotionPending,
		resume:   recoveryruntime.ResumePromotionForOwner,
		selected: func() (launcherRuntime, error) { return recoveryruntime.VerifiedLauncherRuntime() },
	})
}
