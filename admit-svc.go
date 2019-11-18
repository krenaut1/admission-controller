package main

import (
	"errors"
	"fmt"
	"log"

	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	svcResource = metav1.GroupVersionResource{Version: "v1", Resource: "services"}
)

// admitSvc validates and mutates services for windstream standards
func admitSvc(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	var patches []patchOperation
	var msg string
	log.Printf("admitSvc evoked! Namespace: %v Name: %v Group: %v Version: %v Resource: %v Operation: %v\n", req.Namespace, req.Name, req.Resource.Group, req.Resource.Version, req.Resource.Resource, req.Operation)
	raw := req.Object.Raw
	logReq(raw)

	// approve any ingress that is in an un-monitored Namespace
	if !namespaceIsMonitored(req.Namespace) {
		log.Printf("Approved service name: %v namespace: %v. Namespace is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}

	// approve any ingress that is specifically exempt
	if serviceIsExempt(req.Namespace, req.Name) {
		log.Printf("Approved service name: %v namespace: %v. Service is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}
	// This handler should only get called on Pod objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	if req.Resource != svcResource {
		log.Printf("expect resource to be %s", svcResource)
		return nil, nil
	}

	// Parse the Pod object.
	svc := corev1.Service{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &svc); err != nil {
		return nil, fmt.Errorf("could not deserialize pod object: %v", err)
	}

	// Retrieve the name and namespace
	svcMetaData := svc.ObjectMeta
	svcName := svcMetaData.Name
	svcNamespace := svcMetaData.Namespace

	log.Printf("Validating service name: %v namespace: %v\n", svcName, svcNamespace)

	// reject if annotations section is missing
	if svcMetaData.Annotations == nil {
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v. metadata.annotations object is missing\n", svcName, svcNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// try to get svc label if its missing add it.  If its present validate it matches serviceName
	svcLabelValue, ok := svcMetaData.Labels["svc"]
	if !ok {
		// svc label is missing
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v is missing svc label\n", svcName, svcNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}
	if svcLabelValue != svcName {
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v. metadata.labels.svc: %v must match service Name: %v\n", svcName, svcNamespace, svcLabelValue, svcName)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// try to get description annotation
	_, ok = svcMetaData.Annotations["description"]
	if !ok {
		// description annotation is missing
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v is missing description annotation\n", svcName, svcNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// try to get selector
	selectorValue, ok := svc.Spec.Selector["svc"]
	if !ok {
		// svc selector is missing
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v is missing selector svc\n", svcName, svcNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}
	if selectorValue != svcName {
		msg = fmt.Sprintf("Rejected service name: %v namespace: %v. spec.selector.svc: %v must match service Name: %v\n", svcName, svcNamespace, selectorValue, svcName)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	return patches, nil
}
