// agent-native-contract is an offline current-source release producer. Never
// run it over historical/installed Agent binaries to invent compatibility.
package main

import (
	"debug/buildinfo"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
)

func main() {
	binary := flag.String("agent", "bin/agent", "fresh current-source Agent build")
	commit := flag.String("commit", "", "reviewed exact source commit")
	output := flag.String("output", "bin/agent-native-contract.json", "offline contract output")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected arguments")
		os.Exit(2)
	}
	if err := assemble(*binary, *commit, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func assemble(binary, commit, output string) error {
	if filepath.Base(binary) != "agent" || filepath.Base(output) != agentnativecontract.FileName || filepath.Clean(binary) == filepath.Clean(output) {
		return errors.New("separate Agent/contract build outputs required")
	}
	info, err := os.Lstat(binary)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > agentnativecontract.MaxAgentSize {
		return errors.New("unsafe Agent build input")
	}
	f, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("Agent build input changed")
	}
	build, err := buildinfo.Read(f)
	if err != nil || build.Path != "github.com/alicelik/celikpanel/cmd/agent" || build.Main.Path != "github.com/alicelik/celikpanel" {
		return errors.New("normal management Agent build required")
	}
	// Helpers share Agent sources but do not provide its RPC hook writer.
	for _, setting := range build.Settings {
		if setting.Key == "-tags" && setting.Value != "" {
			return errors.New("tagged helper cannot certify management Agent behavior")
		}
	}
	raw, err := io.ReadAll(io.LimitReader(f, agentnativecontract.MaxAgentSize+1))
	if err != nil {
		return err
	}
	after, err := f.Stat()
	pathAfter, e := os.Lstat(binary)
	if err != nil || e != nil || !os.SameFile(info, pathAfter) || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return errors.New("Agent build input changed")
	}
	contract, err := agentnativecontract.New(raw, commit)
	if err != nil {
		return err
	}
	encoded, err := agentnativecontract.Encode(contract)
	if err != nil {
		return err
	}
	if old, err := os.Lstat(output); err == nil {
		if !old.Mode().IsRegular() || old.Size() > agentnativecontract.MaxSize {
			return errors.New("unrecognized prior build contract")
		}
		bytes, e := os.ReadFile(output)
		if e != nil {
			return e
		}
		if _, e = agentnativecontract.Parse(bytes); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(output), ".agent-contract-")
	if err != nil {
		return err
	}
	name := stage.Name()
	defer os.Remove(name)
	defer stage.Close()
	if err = stage.Chmod(0644); err != nil {
		return err
	}
	if _, err = stage.Write(encoded); err != nil {
		return err
	}
	if err = stage.Sync(); err != nil {
		return err
	}
	if err = stage.Close(); err != nil {
		return err
	}
	return os.Rename(name, output)
}
