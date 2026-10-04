package main

import (
	"context"
	"fmt"
	"github.com/alicelik/celikpanel/internal/firewallboot"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 || (os.Args[1] != "--check" && os.Args[1] != "--restore") {
		fmt.Fprintln(os.Stderr, "Usage: firewall-restore --check | --restore")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := firewallboot.Restore(ctx, os.Args[1] == "--check")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Saved firewall policy could not be restored:", err, "Preserve /etc/celikpanel/firewall.nft; inspect native nftables and SSH configuration from the owner console before retrying.")
		os.Exit(1)
	}
	switch {
	case !result.Present:
		fmt.Println("No saved firewall policy; nothing was applied.")
	case result.Applied:
		fmt.Println("Saved firewall policy applied atomically to inet celikpanel_fw.")
	default:
		fmt.Println("Saved firewall policy and current SSH access passed native preflight; nothing was applied.")
	}
}
