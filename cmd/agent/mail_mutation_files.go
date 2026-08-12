package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
)

type mailFileWrite struct {
	path    string
	content []byte
	mode    os.FileMode
}

type mailFileSnapshot struct {
	path    string
	exists  bool
	content []byte
	mode    os.FileMode
	uid     int
	gid     int
}

var (
	mailMutationWrite   = secureWriteConfig
	mailMutationPostmap = postmapReadableContext
)

func applyMailFileMutation(
	ctx context.Context,
	writes []mailFileWrite,
	postmapPaths []string,
	afterWrite func() (func() error, error),
) error {
	// The Dovecot passwd-file contains password hashes. Validate its exact
	// ownership and mode again immediately before the transaction snapshots
	// and writes it. ConfigureMailStack deliberately bypasses this mutation
	// helper because it is the one explicit create/repair path.
	for _, write := range writes {
		if write.path == dovecotUsersPath {
			if err := validateDovecotUsersFileMetadata(write.path, true); err != nil {
				return err
			}
		}
	}

	paths := make(map[string]struct{}, len(writes)+2*len(postmapPaths))
	for _, write := range writes {
		paths[write.path] = struct{}{}
	}
	for _, path := range postmapPaths {
		paths[path] = struct{}{}
		paths[path+".db"] = struct{}{}
		paths[path+".lmdb"] = struct{}{}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	snapshots := make([]mailFileSnapshot, 0, len(ordered))
	for _, path := range ordered {
		snapshot, err := snapshotMailFile(path)
		if err != nil {
			return fmt.Errorf("snapshot mail configuration: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}

	rollback := func(cause error, afterRollback func() error) error {
		var rollbackErrs []error
		if afterRollback != nil {
			if err := afterRollback(); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("rollback mailbox directory: %w", err))
			}
		}
		for _, snapshot := range snapshots {
			if err := restoreMailFile(snapshot); err != nil {
				rollbackErrs = append(rollbackErrs, err)
			}
		}
		if len(rollbackErrs) != 0 {
			return fmt.Errorf("%w; mail configuration rollback failed: %v", cause, errors.Join(rollbackErrs...))
		}
		return cause
	}

	for _, write := range writes {
		if err := mailMutationWrite(write.path, write.content, write.mode); err != nil {
			return rollback(fmt.Errorf("write mail configuration %s: %w", write.path, err), nil)
		}
	}
	for _, path := range postmapPaths {
		if err := mailMutationPostmap(ctx, path); err != nil {
			return rollback(fmt.Errorf("build postfix map %s: %w", path, err), nil)
		}
	}

	if afterWrite != nil {
		afterRollback, err := afterWrite()
		if err != nil {
			return rollback(fmt.Errorf("publish mailbox directory: %w", err), afterRollback)
		}
	}
	return nil
}

func snapshotMailFile(path string) (mailFileSnapshot, error) {
	content, mode, uid, gid, err := secureSnapshotMailFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mailFileSnapshot{path: path}, nil
		}
		return mailFileSnapshot{}, fmt.Errorf("%s: %w", path, err)
	}
	return mailFileSnapshot{
		path:    path,
		exists:  true,
		content: append([]byte(nil), content...),
		mode:    mode,
		uid:     uid,
		gid:     gid,
	}, nil
}

func restoreMailFile(snapshot mailFileSnapshot) error {
	if !snapshot.exists {
		if err := secureRemoveConfig(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove newly-created mail configuration %s: %w", snapshot.path, err)
		}
		return nil
	}
	if err := secureWriteConfig(snapshot.path, snapshot.content, snapshot.mode); err != nil {
		return fmt.Errorf("restore mail configuration %s: %w", snapshot.path, err)
	}
	if err := secureSetMailFileMetadata(snapshot.path, snapshot.mode, snapshot.uid, snapshot.gid); err != nil {
		return fmt.Errorf("restore mail configuration metadata %s: %w", snapshot.path, err)
	}
	return nil
}
