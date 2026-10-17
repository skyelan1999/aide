// aide-message-relay is an optional, self-hosted discovery/opaque-tunnel relay.
// It does not store messages, private keys, or workbench authentication tokens.
package main

import (
	"aide/internal/messaging"
	"crypto/tls"
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	listen := flag.String("listen", ":8443", "HTTPS listen address")
	cert := flag.String("cert", "", "PEM certificate with relay hostname")
	key := flag.String("key", "", "PEM private key")
	flag.Parse()
	if *cert == "" || *key == "" {
		log.Fatal("-cert and -key are required; relay clients verify normal HTTPS certificates")
	}
	relay := messaging.NewRelay()
	defer relay.Close()
	server := &http.Server{Addr: *listen, Handler: relay, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}}}
	log.Printf("aide message rendezvous/opaque relay listening on %s", *listen)
	log.Fatal(server.ListenAndServeTLS(*cert, *key))
}
