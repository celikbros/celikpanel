package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type configTestState struct {
	phpRoot        string
	phpPath        string
	mysqlPath      string
	reloadCalls    int
	restartCalls   int
	phpTestCalls   int
	mysqlTestCalls int
}

func withConfigTestState(t *testing.T, phpBody, mysqlBody string) *configTestState {
	t.Helper()
	root := t.TempDir()
	state := &configTestState{
		phpRoot:   root,
		phpPath:   filepath.Join(root, "8.3", "fpm", "php.ini"),
		mysqlPath: filepath.Join(root, "50-server.cnf"),
	}
	if err := os.MkdirAll(filepath.Dir(state.phpPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.phpPath, []byte(phpBody), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.mysqlPath, []byte(mysqlBody), 0o640); err != nil {
		t.Fatal(err)
	}

	oldRoot := phpEtcDir
	oldMySQLPath := mysqlServerConfigPath
	oldReload := reloadPHPFPM
	oldRestart := restartMariaDB
	oldPHPTest := phpFPMConfigTest
	oldMySQLTest := mysqlConfigTest
	phpEtcDir = root
	mysqlServerConfigPath = state.mysqlPath
	reloadPHPFPM = func(string) error { state.reloadCalls++; return nil }
	restartMariaDB = func() error { state.restartCalls++; return nil }
	phpFPMConfigTest = func(string) error { state.phpTestCalls++; return nil }
	mysqlConfigTest = func() error { state.mysqlTestCalls++; return nil }
	t.Cleanup(func() {
		phpEtcDir = oldRoot
		mysqlServerConfigPath = oldMySQLPath
		reloadPHPFPM = oldReload
		restartMariaDB = oldRestart
		phpFPMConfigTest = oldPHPTest
		mysqlConfigTest = oldMySQLTest
	})
	return state
}

const phpSettingsFixture = `memory_limit = 128M
max_execution_time = 30
upload_max_filesize = 2M
post_max_size = 8M
max_input_vars = 1000
display_errors = Off
`

const mysqlSettingsFixture = `[mysqld]
max_connections = 151
innodb_buffer_pool_size = 128M
query_cache_size = 16M
max_allowed_packet = 16M
`

func TestUpdatePHPSettingsRejectsInjectedLineWithoutWriting(t *testing.T) {
	state := withConfigTestState(t, phpSettingsFixture, mysqlSettingsFixture)
	before, _ := os.ReadFile(state.phpPath)
	err := NewConfigManager().UpdatePHPSettings("8.3", &PHPSettings{
		MemoryLimit: "128M\nallow_url_include = On", MaxExecutionTime: 30,
		UploadMaxFilesize: "2M", PostMaxSize: "8M", MaxInputVars: 1000,
	})
	if err == nil {
		t.Fatal("line-injected PHP value was accepted")
	}
	after, _ := os.ReadFile(state.phpPath)
	if string(after) != string(before) || state.reloadCalls != 0 || state.phpTestCalls != 0 {
		t.Fatalf("invalid PHP request changed or activated configuration")
	}
}

func TestUpdatePHPSettingsReloadFailureRestoresBytesAndMode(t *testing.T) {
	state := withConfigTestState(t, phpSettingsFixture, mysqlSettingsFixture)
	before, _ := os.ReadFile(state.phpPath)
	beforeInfo, _ := os.Stat(state.phpPath)
	reloadPHPFPM = func(string) error {
		state.reloadCalls++
		if state.reloadCalls == 1 {
			return errors.New("reload failed")
		}
		return nil
	}
	err := NewConfigManager().UpdatePHPSettings("8.3", &PHPSettings{
		MemoryLimit: "256M", MaxExecutionTime: 60,
		UploadMaxFilesize: "8M", PostMaxSize: "16M", MaxInputVars: 2000,
	})
	if err == nil || !strings.Contains(err.Error(), "previous configuration restored and activated") {
		t.Fatalf("false success or incomplete rollback error: %v", err)
	}
	after, _ := os.ReadFile(state.phpPath)
	afterInfo, _ := os.Stat(state.phpPath)
	if string(after) != string(before) {
		t.Fatalf("PHP rollback was not byte exact:\n%s", after)
	}
	if beforeInfo.Mode().Perm() != afterInfo.Mode().Perm() {
		t.Fatalf("mode changed from %o to %o", beforeInfo.Mode().Perm(), afterInfo.Mode().Perm())
	}
	if state.reloadCalls != 2 || state.phpTestCalls != 2 {
		t.Fatalf("validation/reload calls = %d/%d, want 2/2", state.phpTestCalls, state.reloadCalls)
	}
}

func TestUpdatePHPSettingsSyntaxFailureRestoresAndReactivatesPreviousConfig(t *testing.T) {
	state := withConfigTestState(t, phpSettingsFixture, mysqlSettingsFixture)
	before, _ := os.ReadFile(state.phpPath)
	phpFPMConfigTest = func(string) error {
		state.phpTestCalls++
		if state.phpTestCalls == 1 {
			return errors.New("syntax failed")
		}
		return nil
	}
	err := NewConfigManager().UpdatePHPSettings("8.3", &PHPSettings{
		MemoryLimit: "256M", MaxExecutionTime: 60,
		UploadMaxFilesize: "8M", PostMaxSize: "16M", MaxInputVars: 2000,
	})
	if err == nil {
		t.Fatal("syntax failure falsely succeeded")
	}
	after, _ := os.ReadFile(state.phpPath)
	if string(after) != string(before) || state.phpTestCalls != 2 || state.reloadCalls != 1 {
		t.Fatalf("syntax rollback did not restore, validate and activate old config")
	}
}

func TestUpdateMySQLSettingsRestartFailureRestoresExactSnapshot(t *testing.T) {
	state := withConfigTestState(t, phpSettingsFixture, mysqlSettingsFixture)
	before, _ := os.ReadFile(state.mysqlPath)
	restartMariaDB = func() error {
		state.restartCalls++
		if state.restartCalls == 1 {
			return errors.New("restart failed")
		}
		return nil
	}
	err := NewConfigManager().UpdateMySQLSettings(&MySQLSettings{
		MaxConnections: 300, InnodbBufferPool: "512M",
		QueryCacheSize: "0", MaxAllowedPacket: "64M",
	})
	if err == nil {
		t.Fatal("database restart failure falsely succeeded")
	}
	after, _ := os.ReadFile(state.mysqlPath)
	if string(after) != string(before) {
		t.Fatalf("database rollback was not byte exact:\n%s", after)
	}
	if state.restartCalls != 2 || state.mysqlTestCalls != 2 {
		t.Fatalf("database validation/restart calls = %d/%d, want 2/2", state.mysqlTestCalls, state.restartCalls)
	}
}

func TestConfigReadersPropagateScannerErrors(t *testing.T) {
	state := withConfigTestState(t, ";"+strings.Repeat("x", 70*1024)+"\n", mysqlSettingsFixture)
	if _, err := NewConfigManager().GetPHPSettings("8.3"); err == nil || !strings.Contains(err.Error(), "scan php.ini") {
		t.Fatalf("scanner error was hidden: %v", err)
	}
	if _, err := NewPHPFPMManager(); err != nil {
		t.Fatal(err)
	}
	_ = state
}

func TestExtendedPHPAdditionalDirectivesAreValidatedAndNotDuplicated(t *testing.T) {
	state := withConfigTestState(t, phpSettingsFixture, mysqlSettingsFixture)
	manager := NewPHPConfigManager()
	config, err := manager.GetExtendedConfig("8.3")
	if err != nil {
		t.Fatal(err)
	}
	config.AdditionalDirectives = "expose_php = Off"
	if err := manager.UpdateExtendedConfig("8.3", config); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdateExtendedConfig("8.3", config); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(state.phpPath)
	if strings.Count(string(body), additionalPHPBegin) != 1 || strings.Count(string(body), "expose_php = Off") != 1 {
		t.Fatalf("managed directive block was duplicated:\n%s", body)
	}
	config.AdditionalDirectives = "memory_limit = 999M"
	if err := manager.UpdateExtendedConfig("8.3", config); err == nil {
		t.Fatal("additional directives overrode a managed key")
	}
}
