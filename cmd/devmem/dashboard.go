package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/arbazkhan971/memorx/internal/dashboard"
)

func runDashboard(args []string) error {
	port := "37778"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port", "-p":
			if i+1 < len(args) {
				port = args[i+1]
				i++
			}
		}
	}

	db, gitRoot, closeDB, err := openProjectDB()
	if err != nil {
		return err
	}
	defer closeDB()

	srv := dashboard.NewServer(db, gitRoot)
	addr := "127.0.0.1:" + port

	fmt.Printf("memorx dashboard: http://%s\n", addr)
	fmt.Println("   press Ctrl+C to stop")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return srv.Serve(ctx, addr)
}
