package main

import (
	"log"
	"net/http"

	"github.com/krenaut1/goconfig"
)

// Config must match config file layout
type Config struct {
	MonitorNamespaces                []string
	ExemptIngresses                  []string
	ExemptDeployments                []string
	ExemptServices                   []string
	ValidHosts                       []string
	NginxMasterIngressAllow          map[string]string
	NginxMinionIngressAllow          map[string]string
	IngressMinionRequiredAnnotations map[string]string
	IngressMinionRequiredLabels      map[string]string
}

var config Config

func main() {
	// load application properties

	err := goconfig.GoConfig(&config)
	if err != nil {
		log.Fatalf("Err thrown: %v\n", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/admit-pod", admitFuncHandler(admitPod))
	mux.Handle("/admit-deploy", admitFuncHandler(admitDeploy))
	mux.Handle("/admit-ing-net", admitFuncHandler(admitIngressNet))
	mux.Handle("/admit-ing-ext", admitFuncHandler(admitIngressExt))
	mux.Handle("/admit-svc", admitFuncHandler(admitSvc))
	server := &http.Server{
		// We listen on port 8443 such that we do not need root privileges or extra capabilities for this server.
		// The Service object will take care of mapping this port to the HTTPS port 443.
		Addr:    ":8443",
		Handler: mux,
	}
	certPath := "/run/secrets/tls/cert.pem"
	keyPath := "/run/secrets/tls/key.pem"
	log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
}
