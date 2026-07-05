package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// BackupInfo represents a backup file
type BackupInfo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Type      string    `json:"type"` // "full", "files", "database"
	CreatedAt time.Time `json:"created_at"`
}

// BackupRequest for creating backups. SourceDir is the site's real document
// root (the agent has no database access, so the panel resolves and passes
// it); when empty the legacy /var/www/<domain> convention is used.
// BackupRequest, yedek oluşturmak içindir. SourceDir sitenin gerçek belge
// köküdür (agent'ın veritabanı erişimi yoktur; panel çözer ve geçirir); boşsa
// eski /var/www/<domain> geleneği kullanılır.
type BackupRequest struct {
	DomainName   string `json:"domain_name"`
	Type         string `json:"type"` // "full", "files", "database"
	DatabaseName string `json:"database_name,omitempty"`
	DatabaseType string `json:"database_type,omitempty"` // "mysql", "postgresql"
	SourceDir    string `json:"source_dir,omitempty"`
}

// BackupResponse contains backup result
type BackupResponse struct {
	Success  bool       `json:"success"`
	Backup   BackupInfo `json:"backup,omitempty"`
	Error    string     `json:"error,omitempty"`
}

// ListBackupsRequest for listing backups
type ListBackupsRequest struct {
	DomainName string `json:"domain_name"`
}

// ListBackupsResponse contains backup list
type ListBackupsResponse struct {
	Backups []BackupInfo `json:"backups"`
}

// RestoreRequest for restoring backups. TargetDir mirrors
// BackupRequest.SourceDir: the panel passes the site's real document root.
// RestoreRequest, yedek geri yüklemek içindir. TargetDir,
// BackupRequest.SourceDir'in karşılığıdır: panel sitenin gerçek belge kökünü
// geçirir.
type RestoreRequest struct {
	DomainName string `json:"domain_name"`
	BackupName string `json:"backup_name"`
	TargetDir  string `json:"target_dir,omitempty"`
}

// DeleteBackupRequest for deleting a backup
type DeleteBackupRequest struct {
	DomainName string `json:"domain_name"`
	BackupName string `json:"backup_name"`
}

// backupBaseDir is where backup archives live. Production default is
// /var/backups/celikpanel (root agent); CELIKPANEL_BACKUP_DIR overrides it so
// a non-root development agent can exercise real backups too.
// backupBaseDir, yedek arşivlerinin yaşadığı yerdir. Üretim varsayılanı
// /var/backups/celikpanel'dir (root agent); CELIKPANEL_BACKUP_DIR bunu
// geçersiz kılar; böylece root olmayan bir geliştirme agent'ı da gerçek
// yedekleri çalıştırabilir.
var backupBaseDir = func() string {
	if d := os.Getenv("CELIKPANEL_BACKUP_DIR"); d != "" {
		return d
	}
	return "/var/backups/celikpanel"
}()

// CreateBackup creates a backup of domain files or database
func (a *Agent) CreateBackup(req *BackupRequest, resp *BackupResponse) error {
	// Ensure backup directory exists
	backupDir := filepath.Join(backupBaseDir, req.DomainName)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("Failed to create backup directory: %v", err)
		return nil
	}

	timestamp := time.Now().Format("20060102_150405")
	var backupPath string
	var backupType string

	sourceDir := req.SourceDir
	if sourceDir == "" {
		sourceDir = filepath.Join("/var/www", req.DomainName)
	}

	switch req.Type {
	case "files":
		backupPath = filepath.Join(backupDir, fmt.Sprintf("files_%s.tar.gz", timestamp))
		backupType = "files"
		if err := a.createFilesBackup(sourceDir, backupPath); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("Failed to create files backup: %v", err)
			return nil
		}

	case "database":
		if req.DatabaseName == "" {
			resp.Success = false
			resp.Error = "Database name is required"
			return nil
		}
		backupPath = filepath.Join(backupDir, fmt.Sprintf("db_%s_%s.sql.gz", req.DatabaseName, timestamp))
		backupType = "database"
		if err := a.createDatabaseBackup(req.DatabaseName, req.DatabaseType, backupPath); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("Failed to create database backup: %v", err)
			return nil
		}

	case "full":
		backupPath = filepath.Join(backupDir, fmt.Sprintf("full_%s.tar.gz", timestamp))
		backupType = "full"
		if err := a.createFullBackup(sourceDir, backupPath); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("Failed to create full backup: %v", err)
			return nil
		}

	default:
		resp.Success = false
		resp.Error = "Invalid backup type"
		return nil
	}

	// Get backup info
	info, err := os.Stat(backupPath)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("Failed to get backup info: %v", err)
		return nil
	}

	resp.Success = true
	resp.Backup = BackupInfo{
		Name:      filepath.Base(backupPath),
		Path:      backupPath,
		Size:      info.Size(),
		Type:      backupType,
		CreatedAt: time.Now(),
	}

	return nil
}

// createFilesBackup creates a tar.gz of the given document root.
// createFilesBackup, verilen belge kökünün tar.gz'sini oluşturur.
func (a *Agent) createFilesBackup(sourceDir, backupPath string) error {
	// Check if source exists
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		return fmt.Errorf("domain directory not found: %s", sourceDir)
	}

	// Create backup file
	file, err := os.Create(backupPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Walk directory and add files
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Update name to be relative
		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		header.Name = relPath

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// If not a regular file, skip content
		if !info.Mode().IsRegular() {
			return nil
		}

		// Copy file content. O_NOFOLLOW closes the TOCTOU gap between the
		// Walk's lstat and this open: if the entry was swapped for a
		// symlink after the IsRegular check above, the open fails instead
		// of following it out of the backup root.
		// Dosya içeriğini kopyala. O_NOFOLLOW, Walk'ın lstat'ı ile bu açış
		// arasındaki TOCTOU boşluğunu kapatır: yukarıdaki IsRegular
		// kontrolünden sonra giriş bir symlink'le değiştirildiyse, açış onu
		// izlemek yerine başarısız olur.
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0) //nosec G122 -- O_NOFOLLOW prevents the symlink TOCTOU on the final component
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(tarWriter, f)
		return err
	})
}

// createDatabaseBackup creates a database dump
func (a *Agent) createDatabaseBackup(dbName, dbType, backupPath string) error {
	var cmd *exec.Cmd

	switch dbType {
	case "mysql", "":
		// mysqldump with compression
		cmd = exec.Command("bash", "-c", 
			fmt.Sprintf("mysqldump --single-transaction --routines --triggers %s | gzip > %s", dbName, backupPath))
	case "postgresql":
		cmd = exec.Command("bash", "-c",
			fmt.Sprintf("pg_dump %s | gzip > %s", dbName, backupPath))
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("backup failed: %v, output: %s", err, string(output))
	}

	return nil
}

// createFullBackup creates a backup of both files and databases
func (a *Agent) createFullBackup(sourceDir, backupPath string) error {
	// For now, just backup files (database would need to be specified separately)
	return a.createFilesBackup(sourceDir, backupPath)
}

// ListBackups lists all backups for a domain
func (a *Agent) ListBackups(req *ListBackupsRequest, resp *ListBackupsResponse) error {
	backupDir := filepath.Join(backupBaseDir, req.DomainName)
	
	resp.Backups = make([]BackupInfo, 0)

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil // No backups yet
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Determine backup type from filename
		backupType := "unknown"
		name := entry.Name()
		if strings.HasPrefix(name, "files_") {
			backupType = "files"
		} else if strings.HasPrefix(name, "db_") {
			backupType = "database"
		} else if strings.HasPrefix(name, "full_") {
			backupType = "full"
		}

		resp.Backups = append(resp.Backups, BackupInfo{
			Name:      name,
			Path:      filepath.Join(backupDir, name),
			Size:      info.Size(),
			Type:      backupType,
			CreatedAt: info.ModTime(),
		})
	}

	// Sort by date descending (newest first)
	sort.Slice(resp.Backups, func(i, j int) bool {
		return resp.Backups[i].CreatedAt.After(resp.Backups[j].CreatedAt)
	})

	return nil
}

// RestoreBackup restores a backup
func (a *Agent) RestoreBackup(req *RestoreRequest, resp *BackupResponse) error {
	backupPath := filepath.Join(backupBaseDir, req.DomainName, req.BackupName)

	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		resp.Success = false
		resp.Error = "Backup not found"
		return nil
	}

	targetDir := req.TargetDir
	if targetDir == "" {
		targetDir = filepath.Join("/var/www", req.DomainName)
	}

	// Determine backup type and restore
	if strings.HasPrefix(req.BackupName, "files_") || strings.HasPrefix(req.BackupName, "full_") {
		if err := a.restoreFilesBackup(targetDir, backupPath); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("Failed to restore: %v", err)
			return nil
		}
	} else if strings.HasPrefix(req.BackupName, "db_") {
		// Extract database name from backup filename (db_DBNAME_timestamp.sql.gz)
		parts := strings.Split(strings.TrimPrefix(req.BackupName, "db_"), "_")
		if len(parts) < 2 {
			resp.Success = false
			resp.Error = "Invalid database backup filename"
			return nil
		}
		dbName := parts[0]
		if err := a.restoreDatabaseBackup(dbName, backupPath); err != nil {
			resp.Success = false
			resp.Error = fmt.Sprintf("Failed to restore database: %v", err)
			return nil
		}
	}

	resp.Success = true
	return nil
}

// restoreFilesBackup extracts a tar.gz backup into the given document root.
// restoreFilesBackup, bir tar.gz yedeğini verilen belge köküne açar.
func (a *Agent) restoreFilesBackup(targetDir, backupPath string) error {
	// Open backup file
	file, err := os.Open(backupPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, header.Name)

		// Security check
		if !strings.HasPrefix(targetPath, targetDir) {
			continue
		}

		// Keep only the permission bits from the archive; masking also
		// makes the int64→FileMode conversion safe from overflow.
		// Arşivden yalnızca izin bitlerini al; maskeleme ayrıca
		// int64→FileMode dönüşümünü taşmadan korur.
		mode := os.FileMode(header.Mode & 0o7777)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, mode); err != nil {
				return err
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()

			// Set permissions
			if err := os.Chmod(targetPath, mode); err != nil {
				return err
			}
		}
	}

	return nil
}

// restoreDatabaseBackup restores a database from backup
func (a *Agent) restoreDatabaseBackup(dbName, backupPath string) error {
	// Decompress and restore
	cmd := exec.Command("bash", "-c",
		fmt.Sprintf("gunzip -c %s | mysql %s", backupPath, dbName))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restore failed: %v, output: %s", err, string(output))
	}

	return nil
}

// DeleteBackup deletes a backup file
func (a *Agent) DeleteBackup(req *DeleteBackupRequest, resp *bool) error {
	backupPath := filepath.Join(backupBaseDir, req.DomainName, req.BackupName)

	// Security check
	if !strings.HasPrefix(backupPath, backupBaseDir) {
		return os.ErrPermission
	}

	if err := os.Remove(backupPath); err != nil {
		return err
	}

	*resp = true
	return nil
}
