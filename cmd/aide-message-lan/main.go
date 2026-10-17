// aide-message-lan runs on the host; TLS and device identity stay in Docker.
package main

import (
	"aide/internal/messaging"
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	dir := flag.String("dir", "", "private bridge directory")
	target := flag.String("target", "", "loopback Docker port mapping")
	status := flag.Bool("status", false, "check the live helper lease")
	stop := flag.Bool("stop", false, "stop the helper owning this directory")
	flag.Parse()
	if *dir == "" {
		log.Fatal("-dir is required")
	}
	if *status {
		if !messaging.LANBridgeReady(*dir) {
			os.Exit(1)
		}
		return
	}
	if *stop {
		if err := messaging.StopLANBridge(*dir); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := messaging.RunLANBridge(ctx, *dir, *target); err != nil {
		log.Fatal(err)
	}
}
