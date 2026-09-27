package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/trayclient"
)

func main() {
	out := make(chan trayclient.Message, 32)
	c := trayclient.New(os.Getenv("XDG_RUNTIME_DIR"), out)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go c.Run(ctx)
	for {
		select {
		case m := <-out:
			if m.Kind == trayclient.KindSnapshot {
				fmt.Printf("snapshot: %d item(s)\n", len(m.Snapshot.Items))
				for _, it := range m.Snapshot.Items {
					fmt.Printf("  %-22s close_supported=%-5v owner=%s\n", it.ID, it.CloseSupported, it.Key.Owner)
				}
				return
			}
		case <-ctx.Done():
			fmt.Println("timeout")
			return
		}
	}
}
