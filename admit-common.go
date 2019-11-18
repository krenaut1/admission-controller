package main

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
)

// match logic is namespace starts with (has prefix of)
func namespaceIsMonitored(ns string) bool {
	for _, monitorNs := range config.MonitorNamespaces {
		if strings.HasPrefix(ns, monitorNs) {
			return true
		}
	}

	return false
}

func logReq(body []byte) {
	var prettyJSON bytes.Buffer
	err := json.Indent(&prettyJSON, body, "", "  ")
	if err != nil {
		log.Println("could not pretty print JSON error: ", err)
		return
	}
	log.Println(string(prettyJSON.Bytes()))
}
