package main

import (
	"log"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func serviceIsExempt(ns string, name string) bool {
	for _, exemptService := range config.ExemptServices {
		if ns+"/"+name == exemptService {
			return true
		}
	}
	return false
}

func deployIsExempt(ns string, name string) bool {
	for _, exemptDeploy := range config.ExemptDeployments {
		if ns+"/"+name == exemptDeploy {
			return true
		}
	}
	return false
}

func ingressIsExempt(ns string, name string) bool {
	for _, exemptIng := range config.ExemptIngresses {
		if ns+"/"+name == exemptIng {
			return true
		}
	}
	return false
}

func hostIsValid(host string) bool {
	for _, validHost := range config.ValidHosts {
		if host == validHost {
			return true
		}
	}
	return false
}

func checkAllowedNginxAnnotations(i *metav1.ObjectMeta, ingType string) (string, string, bool) {
	log.Print("CheckAllowedNginxAnnotations routine is running...")
	var ok bool
	var testRegEx string
	for k, v := range i.Annotations {
		if strings.HasPrefix(k, "nginx.org/") ||
			strings.HasPrefix(k, "nginx.com/") ||
			strings.HasPrefix(k, "custom.nginx.org/") {
			if ingType == "master" {
				testRegEx, ok = config.NginxMasterIngressAllow[k]
			} else {
				testRegEx, ok = config.NginxMinionIngressAllow[k]
			}
			// if we found an nginx annotation that is not allowed return it with ok = false
			if !ok {
				log.Printf("ingress contains %v annotation, but its not in the nginx allowed list", k)
				return k, v, ok
			}
			// compile the regex string from the app config
			re, err := regexp.Compile(testRegEx)
			// if the regex won't compile then log the error and skip testing this value
			if err != nil {
				log.Printf("Unable to validate %v ingress annotation: %v regex configuration %v is invalid err: %v\n", ingType, k, testRegEx, err.Error())
			} else {
				// test if annotation value matches configured regular expression
				if !re.Match([]byte(v)) {
					log.Printf("annotation name: %v value: %v did not match regex %v\n", k, v, testRegEx)
					return k, v, false
				}
			}

		}
	}
	return "", "", true
}
func checkMinionRequiredNginxAnnotations(i *metav1.ObjectMeta) (string, string, bool) {
	log.Print("CheckMinionRequiredNginxAnnotations routine is running...")
	var ok bool
	var reqValue string
	for k, v := range config.IngressMinionRequiredAnnotations {
		reqValue, ok = i.Annotations[k]
		// throw error if required annotation is not found
		if !ok {
			return k, "", false
		}
		// compile the regex string from the app config
		re, err := regexp.Compile(v)
		// if the regex won't compile then log the error and skip testing this value
		if err != nil {
			log.Printf("Unable to validate ingress annotation: %v regex configuration %v is invalid err: %v\n", k, v, err.Error())
		} else {
			// test if annotation value matches configured regular expression
			if !re.Match([]byte(reqValue)) {
				log.Printf("annotation name: %v value: %v did not match regex %v\n", k, reqValue, v)
				return k, reqValue, false
			}
		}
	}
	return "", "", true
}
func checkMinionRequiredLabels(i *metav1.ObjectMeta) (string, string, bool) {
	log.Printf("chekcMinionRequiredLabels is running...")
	var ok bool
	var reqValue string
	for k, v := range config.IngressMinionRequiredLabels {
		reqValue, ok = i.Labels[k]
		// throw error if required label is not found
		if !ok {
			return k, "", false
		}
		// compile the regex string from the app config
		re, err := regexp.Compile(v)
		// if the regex won't compile then log the error and skip testing this value
		if err != nil {
			log.Printf("Unable to validate ingress label: %v regex configuration %v is invalid err: %v\n", k, v, err.Error())
		} else {
			// test if annotation value matches configured regular expression
			if !re.Match([]byte(reqValue)) {
				log.Printf("annotation name: %v value: %v did not match regex %v\n", k, reqValue, v)
				return k, reqValue, false
			}
		}
	}
	return "", "", true
}
