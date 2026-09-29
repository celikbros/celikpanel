// Read-only fixture tool (not product code): one authenticated
// Agent.ServiceMutationStatus call for one request ID, printed as JSON.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: oi-smstatus <request-id>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := transport.ConnectAgentContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	defer client.Close()
	var response transport.ServiceMutationResponse
	if err := client.Call("Agent.ServiceMutationStatus", &transport.ServiceMutationStatusRequest{RequestID: os.Args[1]}, &response); err != nil {
		fmt.Fprintln(os.Stderr, "call:", err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "request_id": os.Args[1], "response": response}, "", "  ")
	fmt.Println(string(out))
}
