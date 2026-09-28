//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/bindpeerinspector"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "native BIND peer observation unavailable")
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 1 || (os.Getenv("SSH_ORIGINAL_COMMAND") != "" && os.Getenv("SSH_ORIGINAL_COMMAND") != "celikpanel-bind-peer-inspect-v1") {
		return errors.New("inspector accepts no command")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return runWith(ctx, os.Stdin, os.Stdout, bindpeerinspector.OwnerPolicyReader{}, bindpeerinspector.NativeReader{}, time.Now)
}

func runWith(ctx context.Context, input io.Reader, output io.Writer, policy bindpeerinspector.PolicyReader, native bindpeerinspector.Reader, now func() time.Time) error {
	raw, err := io.ReadAll(io.LimitReader(input, 4098))
	if err != nil || len(raw) > 4097 {
		return errors.New("request exceeds safe bound")
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	request, err := dnspeerproof.DecodeRequest(raw)
	if err != nil {
		return err
	}
	response, err := bindpeerinspector.Inspect(ctx, request, native, policy, now)
	if err != nil {
		return err
	}
	encoded, err := dnspeerproof.EncodeResponse(response)
	if err != nil {
		return err
	}
	_, err = output.Write(append(encoded, '\n'))
	return err
}
