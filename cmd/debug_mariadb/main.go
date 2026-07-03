package main

import (
	"fmt"
	"log"
	"os/exec"
)

type MariaDBDriver struct {
	host         string
	port         int
	rootPassword string
}

func (d *MariaDBDriver) executeSQL(sql string) error {
	cmd := exec.Command("mysql", "-u", "root", fmt.Sprintf("-p%s", d.rootPassword), "-e", sql)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("SQL error: %v, output: %s", err, string(output))
	}
	return nil
}

func (d *MariaDBDriver) DeleteDatabase(name string) error {
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", name)
	return d.executeSQL(sql)
}

func main() {
	driver := &MariaDBDriver{
		host:         "localhost",
		port:         3306,
		rootPassword: "1234",
	}

	dbName := "1_testdb_1"
	fmt.Printf("Attempting to delete database: %s\n", dbName)

	if err := driver.DeleteDatabase(dbName); err != nil {
		log.Fatalf("Failed to delete database: %v", err)
	}

	fmt.Println("Successfully deleted database!")
}
