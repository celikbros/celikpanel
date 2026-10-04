//go:build linux && amd64

// Controlled, disposable feasibility workload. No product migrations are changed.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "--helper" {
		fmt.Println("WALTRACE_HELPER_READY")
		for {
			time.Sleep(time.Second)
		}
	}
	if len(os.Args) != 3 || (os.Args[1] != "spill" && os.Args[1] != "small" && os.Args[1] != "hang") {
		return fmt.Errorf("controlled fixture mode and private work directory required")
	}
	mode, root := os.Args[1], os.Args[2]
	runtime.GOMAXPROCS(4)
	ready := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			runtime.LockOSThread()
			ready <- true
			for {
				time.Sleep(time.Millisecond * 20)
			}
		}()
	}
	for i := 0; i < 3; i++ {
		<-ready
	}
	helper := exec.Command(os.Args[0], "--helper")
	helper.Stdout, helper.Stderr = os.Stdout, os.Stderr
	if err := helper.Start(); err != nil {
		return err
	}
	defer func() { _ = helper.Process.Kill(); _ = helper.Wait() }()
	if mode == "hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	database, err := sql.Open("sqlite", filepath.Join(root, "fixture.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA page_size=4096", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL",
		"PRAGMA wal_autocheckpoint=0", "PRAGMA cache_size=8", "PRAGMA cache_spill=ON",
		"CREATE TABLE samples (id INTEGER PRIMARY KEY, payload BLOB NOT NULL)",
		"INSERT INTO samples VALUES (1, X'01')",
	} {
		if _, err = database.Exec(statement); err != nil {
			return err
		}
	}
	fmt.Println("WALTRACE_BASELINE_READY")
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	fmt.Println("WALTRACE_TRANSACTION_BEGUN")
	statement := "WITH RECURSIVE numbers(n) AS (VALUES(2) UNION ALL SELECT n+1 FROM numbers WHERE n<1001) INSERT INTO samples SELECT n, zeroblob(3000) FROM numbers"
	if mode == "small" {
		statement = "INSERT INTO samples VALUES (2, X'02')"
	}
	if _, err = transaction.Exec(statement); err != nil {
		return err
	}
	fmt.Println("WALTRACE_COMMIT_ENTERING")
	if err = transaction.Commit(); err != nil {
		return err
	}
	fmt.Println("WALTRACE_COMMIT_RETURNED")
	return nil
}
