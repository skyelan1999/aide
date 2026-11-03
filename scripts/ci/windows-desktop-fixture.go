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
	"time"
)

func main() {
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

const page = `<!doctype html><meta charset="utf-8"><title>Aide desktop fixture</title><style>body{font:18px system-ui;padding:40px;background:#17202c;color:#edf0f5}button{padding:12px}</style><h1>Aide Windows desktop · isolated UI check</h1><button id="open">Open plugin in app tab</button><p id="result">Fixture data only</p><script>
window.fixtureReady=true;window.fixtureNativeReady=false;
if(new URLSearchParams(location.hash.slice(1)).has('pluginWorkbench')){
 document.title='Fixture plugin · aide';const frame=document.createElement('iframe');frame.sandbox='allow-scripts allow-downloads';frame.src='/plugin-workbench/fixture';addEventListener('message',e=>{if(e.source!==frame.contentWindow||e.data!=='fixture-ready')return;const channel=new MessageChannel();channel.port1.onmessage=e=>{window.fixturePluginReady=e.data==='isolated-port-ready'&&localStorage.getItem('desktop-fixture')==='shared';document.getElementById('result').textContent='Native app tab + sandbox MessagePort ready';};frame.contentWindow.postMessage('connect','*',[channel.port2]);});document.body.append(frame);
 window.fixtureNativeBlocked=typeof AideNativeBiometrics==='undefined';
}else{
 window.fixtureNativePresent=typeof AideNativeBiometrics==='object';AideNativeBiometrics.status().then(s=>{window.fixtureNativeReady=typeof s.available==='boolean';});
 document.getElementById('open').onclick=()=>{localStorage.setItem('desktop-fixture','shared');window.open('/#pluginWorkbench=fixture&workspace=local%7CC%3A%5Ctest%7C','_blank');};
}
</script>`
