//go:build linux

package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"golang.org/x/sys/unix"
)

const maxSiteFileBytes = 10 << 20

const secureSiteFileResolve = unix.RESOLVE_BENEATH |
	unix.RESOLVE_NO_SYMLINKS |
	unix.RESOLVE_NO_MAGICLINKS |
	unix.RESOLVE_NO_XDEV

var siteFileDocumentRoot = hostingpath.DocumentRoot

func normalizeSiteFileRelative(raw string, allowRoot bool) (string, error) {
	if strings.ContainsRune(raw, '\x00') {
		return "", os.ErrPermission
	}
	normalized := strings.ReplaceAll(raw, `\`, "/")
	if path.IsAbs(normalized) {
		return "", os.ErrPermission
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return "", os.ErrPermission
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." {
		if allowRoot {
			return "", nil
		}
		return "", os.ErrPermission
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", os.ErrPermission
	}
	return cleaned, nil
}

func openSiteFileRoot(subscriptionID, domainID int) (int, error) {
	documentRoot, err := siteFileDocumentRoot(subscriptionID, domainID)
	if err != nil {
		return -1, os.ErrPermission
	}
	if !path.IsAbs(documentRoot) || path.Clean(documentRoot) != documentRoot {
		return -1, os.ErrPermission
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	defer unix.Close(rootFD)

	fd, err := unix.Openat2(rootFD, strings.TrimPrefix(documentRoot, "/"), &unix.OpenHow{
		Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH |
			unix.RESOLVE_NO_SYMLINKS |
			unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, fmt.Errorf("open immutable site document root: %w", err)
	}
	return fd, nil
}

func openSiteFileAt(rootFD int, relative string, flags uint64) (int, error) {
	lookup := relative
	if lookup == "" {
		lookup = "."
	}
	fd, err := unix.Openat2(rootFD, lookup, &unix.OpenHow{
		Flags:   flags | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: secureSiteFileResolve,
	})
	if err != nil {
		return -1, err
	}
	return fd, nil
}

func openSiteFileParent(rootFD int, relative string) (int, string, error) {
	parent, base := path.Split(relative)
	if base == "" || base == "." || base == ".." || strings.Contains(base, "/") {
		return -1, "", os.ErrPermission
	}
	parent = strings.TrimSuffix(parent, "/")
	parentFD, err := openSiteFileAt(rootFD, parent, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return -1, "", err
	}
	return parentFD, base, nil
}

func siteFileIdentity(reqSubscriptionID, reqDomainID int) (int, error) {
	if reqSubscriptionID <= 0 || reqDomainID <= 0 {
		return -1, os.ErrPermission
	}
	return openSiteFileRoot(reqSubscriptionID, reqDomainID)
}

func siteFileMode(statMode uint32) os.FileMode {
	mode := os.FileMode(statMode & 0o777)
	switch statMode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= os.ModeDir
	case unix.S_IFLNK:
		mode |= os.ModeSymlink
	case unix.S_IFIFO:
		mode |= os.ModeNamedPipe
	case unix.S_IFSOCK:
		mode |= os.ModeSocket
	case unix.S_IFBLK:
		mode |= os.ModeDevice
	case unix.S_IFCHR:
		mode |= os.ModeDevice | os.ModeCharDevice
	}
	return mode
}

func secureListSiteFiles(req *ListFilesRequest, resp *ListFilesResponse) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, true)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	directoryFD, err := openSiteFileAt(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(directoryFD), "site-file-directory")
	if directory == nil {
		unix.Close(directoryFD)
		return os.ErrInvalid
	}
	defer directory.Close()

	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	resp.Path = relative
	resp.Files = make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		var stat unix.Stat_t
		if err := unix.Fstatat(directoryFD, entry.Name(), &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			return err
		}
		mode := siteFileMode(stat.Mode)
		entryPath := path.Join(relative, entry.Name())
		fileInfo := FileInfo{
			Name:        entry.Name(),
			Path:        entryPath,
			IsDir:       mode.IsDir(),
			Size:        stat.Size,
			Permissions: mode.String(),
			ModTime:     time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec),
			Owner:       strconv.FormatUint(uint64(stat.Uid), 10),
			Group:       strconv.FormatUint(uint64(stat.Gid), 10),
		}
		resp.Files = append(resp.Files, fileInfo)
	}
	sort.Slice(resp.Files, func(i, j int) bool {
		if resp.Files[i].IsDir != resp.Files[j].IsDir {
			return resp.Files[i].IsDir
		}
		return strings.ToLower(resp.Files[i].Name) < strings.ToLower(resp.Files[j].Name)
	})
	return nil
}

func secureReadSiteFile(req *ReadFileRequest, resp *ReadFileResponse) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, false)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	fd, err := openSiteFileAt(rootFD, relative, unix.O_RDONLY)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), "site-file")
	if file == nil {
		unix.Close(fd)
		return os.ErrInvalid
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Size < 0 || stat.Size > maxSiteFileBytes {
		return os.ErrInvalid
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSiteFileBytes+1))
	if err != nil {
		return err
	}
	if len(content) > maxSiteFileBytes {
		return os.ErrInvalid
	}
	resp.Path = relative
	resp.Size = int64(len(content))
	probeLength := len(content)
	if probeLength > 512 {
		probeLength = 512
	}
	resp.IsBinary = strings.IndexByte(string(content[:probeLength]), 0) >= 0
	if resp.IsBinary {
		resp.Content = base64.StdEncoding.EncodeToString(content)
	} else {
		resp.Content = string(content)
	}
	return nil
}

func atomicWriteSiteFile(rootFD int, relative string, content []byte) error {
	if len(content) > maxSiteFileBytes {
		return os.ErrInvalid
	}
	parentFD, base, err := openSiteFileParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	var parentStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return err
	}
	uid, gid := int(parentStat.Uid), int(parentStat.Gid)
	mode := uint32(0o644)
	existingFD, openErr := openSiteFileAt(parentFD, base, unix.O_RDONLY)
	if openErr == nil {
		var existingStat unix.Stat_t
		statErr := unix.Fstat(existingFD, &existingStat)
		unix.Close(existingFD)
		if statErr != nil {
			return statErr
		}
		if existingStat.Mode&unix.S_IFMT != unix.S_IFREG {
			return os.ErrInvalid
		}
		uid, gid = int(existingStat.Uid), int(existingStat.Gid)
		mode = existingStat.Mode & 0o777
	} else if !errors.Is(openErr, unix.ENOENT) {
		return openErr
	}

	var randomBytes [12]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return err
	}
	tempName := fmt.Sprintf(".celikpanel-write-%x", randomBytes[:])
	tempFD, err := unix.Openat(
		parentFD,
		tempName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return err
	}
	tempExists := true
	defer func() {
		if tempFD >= 0 {
			_ = unix.Close(tempFD)
		}
		if tempExists {
			_ = unix.Unlinkat(parentFD, tempName, 0)
		}
	}()
	if err := unix.Fchown(tempFD, uid, gid); err != nil {
		return err
	}
	if err := unix.Fchmod(tempFD, mode); err != nil {
		return err
	}
	tempFile := os.NewFile(uintptr(tempFD), "site-file-stage")
	if tempFile == nil {
		return os.ErrInvalid
	}
	if _, err := tempFile.Write(content); err != nil {
		return err
	}
	if err := tempFile.Sync(); err != nil {
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	tempFD = -1
	if err := unix.Renameat2(parentFD, tempName, parentFD, base, 0); err != nil {
		return err
	}
	tempExists = false
	return unix.Fsync(parentFD)
}

func secureWriteSiteFile(req *WriteFileRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, false)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	if err := atomicWriteSiteFile(rootFD, relative, []byte(req.Content)); err != nil {
		return err
	}
	*resp = true
	return nil
}

func secureCreateSiteFile(req *CreateRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, false)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, base, err := openSiteFileParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var parentStat unix.Stat_t
	if err := unix.Fstat(parentFD, &parentStat); err != nil {
		return err
	}
	if req.IsDir {
		if err := unix.Mkdirat(parentFD, base, 0o755); err != nil {
			return err
		}
		createdFD, err := openSiteFileAt(parentFD, base, unix.O_RDONLY|unix.O_DIRECTORY)
		if err != nil {
			_ = unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
		defer unix.Close(createdFD)
		if err := unix.Fchown(createdFD, int(parentStat.Uid), int(parentStat.Gid)); err != nil {
			_ = unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
		if err := unix.Fsync(createdFD); err != nil {
			return err
		}
	} else {
		fd, err := unix.Openat(parentFD, base, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o644)
		if err != nil {
			return err
		}
		file := os.NewFile(uintptr(fd), "new-site-file")
		if file == nil {
			unix.Close(fd)
			_ = unix.Unlinkat(parentFD, base, 0)
			return os.ErrInvalid
		}
		if err := unix.Fchown(fd, int(parentStat.Uid), int(parentStat.Gid)); err != nil {
			file.Close()
			_ = unix.Unlinkat(parentFD, base, 0)
			return err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			_ = unix.Unlinkat(parentFD, base, 0)
			return err
		}
		if err := file.Close(); err != nil {
			_ = unix.Unlinkat(parentFD, base, 0)
			return err
		}
	}
	if err := unix.Fsync(parentFD); err != nil {
		return err
	}
	*resp = true
	return nil
}

func removeSiteFileEntryAt(parentFD int, name string) error {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return os.ErrPermission
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return unix.Unlinkat(parentFD, name, 0)
	}
	childFD, err := openSiteFileAt(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	var openedStat unix.Stat_t
	if err := unix.Fstat(childFD, &openedStat); err != nil {
		unix.Close(childFD)
		return err
	}
	child := os.NewFile(uintptr(childFD), "site-file-delete-directory")
	if child == nil {
		unix.Close(childFD)
		return os.ErrInvalid
	}
	names, err := child.Readdirnames(-1)
	if err != nil {
		child.Close()
		return err
	}
	for _, childName := range names {
		if err := removeSiteFileEntryAt(childFD, childName); err != nil {
			child.Close()
			return err
		}
	}
	if err := unix.Fsync(childFD); err != nil {
		child.Close()
		return err
	}
	if err := child.Close(); err != nil {
		return err
	}
	var currentStat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &currentStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if currentStat.Dev != openedStat.Dev || currentStat.Ino != openedStat.Ino {
		return unix.EBUSY
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}

func secureDeleteSiteFile(req *DeleteRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, false)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, base, err := openSiteFileParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := removeSiteFileEntryAt(parentFD, base); err != nil {
		return err
	}
	if err := unix.Fsync(parentFD); err != nil {
		return err
	}
	*resp = true
	return nil
}

func secureChmodSiteFile(req *ChmodRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	relative, err := normalizeSiteFileRelative(req.Path, false)
	if err != nil {
		return err
	}
	mode, err := parseOctalPermissions(req.Permissions)
	if err != nil || mode > 0o777 {
		return os.ErrInvalid
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	fd, err := openSiteFileAt(rootFD, relative, unix.O_RDONLY)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	fileType := stat.Mode & unix.S_IFMT
	if fileType != unix.S_IFREG && fileType != unix.S_IFDIR {
		return os.ErrInvalid
	}
	if err := unix.Fchmod(fd, uint32(mode)); err != nil {
		return err
	}
	if err := unix.Fsync(fd); err != nil {
		return err
	}
	*resp = true
	return nil
}

func secureRenameSiteFile(req *RenameRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	oldRelative, err := normalizeSiteFileRelative(req.OldPath, false)
	if err != nil {
		return err
	}
	newRelative, err := normalizeSiteFileRelative(req.NewPath, false)
	if err != nil {
		return err
	}
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	oldParentFD, oldBase, err := openSiteFileParent(rootFD, oldRelative)
	if err != nil {
		return err
	}
	defer unix.Close(oldParentFD)
	newParentFD, newBase, err := openSiteFileParent(rootFD, newRelative)
	if err != nil {
		return err
	}
	defer unix.Close(newParentFD)
	if err := unix.Renameat2(oldParentFD, oldBase, newParentFD, newBase, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := unix.Fsync(oldParentFD); err != nil {
		return err
	}
	if newParentFD != oldParentFD {
		if err := unix.Fsync(newParentFD); err != nil {
			return err
		}
	}
	*resp = true
	return nil
}

func secureUploadSiteFile(req *UploadFileRequest, resp *bool) error {
	if req == nil || resp == nil {
		return os.ErrInvalid
	}
	if req.Name == "" || path.Base(req.Name) != req.Name || req.Name == "." || req.Name == ".." || strings.Contains(req.Name, `\`) {
		return os.ErrInvalid
	}
	directory, err := normalizeSiteFileRelative(req.Path, true)
	if err != nil {
		return err
	}
	if base64.StdEncoding.DecodedLen(len(req.Content)) > maxSiteFileBytes {
		return os.ErrInvalid
	}
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil || len(content) > maxSiteFileBytes {
		return os.ErrInvalid
	}
	relative := path.Join(directory, req.Name)
	rootFD, err := siteFileIdentity(req.SubscriptionID, req.DomainID)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	if err := atomicWriteSiteFile(rootFD, relative, content); err != nil {
		return err
	}
	*resp = true
	return nil
}
