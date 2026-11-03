// Synthetic loopback TLS fixture for the separate Windows desktop UI tester.
// It never starts Docker, reads user configuration, or performs real auth.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	if strings.EqualFold(filepath.Base(os.Args[0]), "docker.exe") {
		fakeDocker()
		return
	}

	ready := flag.String("ready", "", "private test readiness file")
	flag.Parse()
	if *ready == "" {
		panic("missing readiness file")
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	path := filepath.Join(filepath.Dir(*ready), "fixture-cert.pem")
	if err = os.WriteFile(path, certificate, 0600); err != nil {
		panic(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	origin := fmt.Sprintf("https://localhost:%d", listener.Addr().(*net.TCPAddr).Port)
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture-ui-ready", func(w http.ResponseWriter, r *http.Request) {
		os.WriteFile(filepath.Join(filepath.Dir(*ready), "ui-ready"), []byte("ready"), 0600)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc("/plugin-workbench/fixture", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><script>addEventListener('message',e=>{if(e.data==='connect'&&e.ports[0])e.ports[0].postMessage('isolated-port-ready')});parent.postMessage('fixture-ready','*')</script><h2>Isolated plugin fixture</h2>`)
	})
	data, _ := json.Marshal(map[string]string{"origin": origin, "certificate": path})
	if err = os.WriteFile(*ready, data, 0600); err != nil {
		panic(err)
	}
	server := http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	err = server.Serve(tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}))
	if err != nil {
		panic(err)
	}
}

const page = `<!doctype html><meta charset="utf-8"><title>Aide desktop fixture</title><style>:root{--bg:#202328;--panel:#24282e;--selected:#344252;--text:#e0e4eb;--muted:#a4adb9;--line:#373c44}html[data-theme="light"]{--bg:#f2f3f5;--panel:#f8f9fa;--selected:#e2e8f0;--text:#262a32;--muted:#626c7b;--line:#d8dee7}body{font:18px system-ui;padding:40px;background:var(--bg);color:var(--text)}button{padding:12px}</style><h1>Aide Windows desktop · isolated UI check</h1><button id="theme" onclick="document.documentElement.dataset.theme=document.documentElement.dataset.theme==='light'?'dark':'light'">切换明暗</button> <button id="open">Open plugin in app tab</button><p id="result">Fixture data only</p><script>
if(new URLSearchParams(location.hash.slice(1)).has('token'))history.replaceState(null,'',location.pathname+location.search);
window.fixtureReady=true;window.fixtureNativeReady=false;
if(new URLSearchParams(location.hash.slice(1)).has('pluginWorkbench')){
 document.title='Fixture plugin · aide';const frame=document.createElement('iframe');frame.sandbox='allow-scripts allow-downloads';frame.src='/plugin-workbench/fixture';addEventListener('message',e=>{if(e.source!==frame.contentWindow||e.data!=='fixture-ready')return;const channel=new MessageChannel();channel.port1.onmessage=e=>{window.fixturePluginReady=e.data==='isolated-port-ready'&&localStorage.getItem('desktop-fixture')==='shared';document.getElementById('result').textContent='Native app tab + sandbox MessagePort ready';};frame.contentWindow.postMessage('connect','*',[channel.port2]);});document.body.append(frame);
 window.fixtureNativeBlocked=typeof AideNativeBiometrics==='undefined';
}else{
 window.fixtureNativePresent=typeof AideNativeBiometrics==='object';AideNativeBiometrics.status().then(s=>{window.fixtureNativeReady=typeof s.available==='boolean';fetch('/fixture-ui-ready');});
 document.getElementById('open').onclick=()=>{localStorage.setItem('desktop-fixture','shared');window.open('/#pluginWorkbench=fixture&workspace=local%7CC%3A%5Ctest%7C','_blank');};
}
</script>`

// Disposable command transport for exercising the unmodified production CLI.
// Only the test runner selects this executable on PATH. No production test mode.
func fakeDocker() {
	raw, err := os.ReadFile(os.Getenv("AIDE_FIXTURE_READY"))
	if err != nil {
		panic(err)
	}
	var ready map[string]string
	if json.Unmarshal(raw, &ready) != nil {
		panic("bad fixture")
	}
	args := os.Args[1:]
	joined := strings.Join(args, " ")
	switch {
	case len(args) > 0 && args[0] == "info":
		fmt.Println(`{"OSType":"linux","Architecture":"amd64","MemTotal":8589934592}`)
	case len(args) > 0 && args[0] == "ps":
		return
	case strings.HasPrefix(joined, "compose version"):
		fmt.Println("Docker Compose fixture")
	case strings.HasPrefix(joined, "compose config"):
		fmt.Println(`{"name":"aide-fixture","services":{"aide":{"volumes":[]}}}`)
	case strings.HasPrefix(joined, "compose port"):
		fmt.Println("127.0.0.1:" + strings.TrimPrefix(ready["origin"], "https://localhost:"))
	case strings.Contains(joined, " curl "):
		fmt.Println(`{"service":"aide","status":"ok"}`)
	case strings.HasSuffix(joined, "cat /data/certs/cert.pem"):
		cert, _ := os.ReadFile(ready["certificate"])
		fmt.Print(string(cert))
	case strings.HasSuffix(joined, "cat /data/auth/access-token"):
		fmt.Print(strings.Repeat("fixture-", 6))
	default:
		fmt.Fprintln(os.Stderr, "Unsupported disposable Docker command "+strconv.Quote(joined))
		os.Exit(2)
	}
}
