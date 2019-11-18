package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"k8s.io/api/admission/v1beta1"
	networkv1beta1 "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ingressNetworkingResource = metav1.GroupVersionResource{Group: "networking.k8s.io", Version: "v1beta1", Resource: "ingresses"}
)

// admitIngress validates and mutates ingresses for windstream standards
// the application configuration contains a list of exempt namespaces
// additionally the controller will not process anything publicly known as a kubernetes namespace
// The rules implemented are
// 1) reject ingresses with unapproved nginx annotations
// 2) require certain nginx annotations
// 3) reject ingresses with rules hostnames that do not match configured master ingresses
// 4) require certain labels, add svc label if missing or mutate it if its invalid
// 5) reject ingresses if the annotation values are malformed
// 6) reject ingresses with more than one rules host
// 7) reject ingresses (and possibly mutate) with rules paths that do not conform to standards
// 8) reject ingresses where the ingress name does not match the rules backend service name
// 9) reject ingresses where the svc label does not match the rules backend service name or add it if not supplied
// 10) reject ingresses that specify the deprecated extenstions/v1beta1 apiVersion

func admitIngressNet(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	var msg string
	// declare patchOperation array as may want to mutate this ingress
	var patches []patchOperation
	// This handler should only get called on ingress objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	log.Printf("admitIngress evoked! Namespace: %v Name: %v Group: %v Version: %v Resource: %v Operation: %v\n", req.Namespace, req.Name, req.Resource.Group, req.Resource.Version, req.Resource.Resource, req.Operation)
	raw := req.Object.Raw
	logReq(raw)

	// approve any ingress that is in an exempt Namespace
	if !namespaceIsMonitored(req.Namespace) {
		log.Printf("Approved ingress name: %v namespace: %v. Namespace is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}

	// approve any ingress that is specifically exempt
	if ingressIsExempt(req.Namespace, req.Name) {
		log.Printf("Approved ingress name: %v namespace: %v. Ingress is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}

	// anything other than ingresses networking.k8s.io v1beta1 should not get here.  But approve if it does for some reason.
	if req.Resource != ingressNetworkingResource {
		log.Printf("Expected resource is %v, received %v. Cannot process, so approving.", ingressNetworkingResource, req.Resource)
		return nil, nil
	}

	// Parse the Ingress object.
	ingress := networkv1beta1.Ingress{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &ingress); err != nil {
		return nil, fmt.Errorf("could not deserialize ingress object: %v, ingress is being rejected", err)
	}

	// Retrieve the name and namespace
	ingMetaData := ingress.ObjectMeta
	ingName := ingMetaData.Name
	ingNamespace := ingMetaData.Namespace

	log.Printf("Validating ingress name: %v namespace: %v\n", ingName, ingNamespace)

	// reject if rules object is missing
	if ingress.Spec.Rules == nil {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. Rules object is missing\n", ingName, ingNamespace)
		log.Printf(msg)
		return nil, errors.New(msg)
	}

	// reject if more than one rules section
	if len(ingress.Spec.Rules) != 1 {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. Rules array has more than one entry specified\n", ingName, ingNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if rules.host is invalid
	if !(hostIsValid(ingress.Spec.Rules[0].Host)) {
		msg = fmt.Sprintf("Reject ingress name: %v namespace: %v.  spec.rules.host: %v is not a known hostname.\n", ingName, ingNamespace, ingress.Spec.Rules[0].Host)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if annotations section is missing
	if ingMetaData.Annotations == nil {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotations object is missing\n", ingName, ingNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if nginx.org/mergeable-ingress-type is missing or not master or minion
	ingType, ok := ingMetaData.Annotations["nginx.org/mergeable-ingress-type"]
	if !ok {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotations nginx.org/mergeable-ingress-type is missing\n", ingName, ingNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}
	if !(ingType == "master" || ingType == "minion") {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotations nginx.org/mergeable-ingress-type: %v is invalid\n", ingName, ingNamespace, ingType)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject ingresses that contain unapproved nginx annotations
	badAnnotationKey, badAnnotationValue, ok := checkAllowedNginxAnnotations(&ingMetaData, ingType)
	if !ok {
		msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotation.%v: %v is invalid or not allowed\n", ingName, ingNamespace, badAnnotationKey, badAnnotationValue)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// these ingress tests only apply to minion ingresses
	if ingType == "minion" {
		// reject if spec.rules.http is missing
		if ingress.Spec.Rules[0].HTTP == nil {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. spec.rules.http object is missing\n", ingName, ingNamespace)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// reject if spec.rules.http.paths is missing
		if ingress.Spec.Rules[0].HTTP.Paths == nil {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. spec.rules.http.paths object is missing\n", ingName, ingNamespace)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// reject if spec.rules.http.paths does not have exactly one entry
		if len(ingress.Spec.Rules[0].HTTP.Paths) != 1 {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. spec.rules.http.paths array has more than one entry specified\n", ingName, ingNamespace)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		path := ingress.Spec.Rules[0].HTTP.Paths[0].Path
		serviceName := ingress.Spec.Rules[0].HTTP.Paths[0].Backend.ServiceName

		// reject if path not equal to /ingNamespace/serviceName/
		expectedPath := "/" + ingNamespace + "/" + serviceName + "/"
		if path != expectedPath {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. spec.rules.http.paths.path is %v but expected %v\n", ingName, ingNamespace, path, expectedPath)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// reject minion ingress if it is missing required annotations or the value is bad
		reqAnnotation, reqValue, ok := checkMinionRequiredNginxAnnotations(&ingMetaData)
		if !ok {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotation.%v: %v is missing or invalid\n", ingName, ingNamespace, reqAnnotation, reqValue)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// reject if minion nginx.org/ssl-services value does not match backend service name
		sslSvc, ok := ingMetaData.Annotations["nginx.org/ssl-services"]
		if !ok {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotations nginx.org/ssl-services is missing\n", ingName, ingNamespace)
			log.Print(msg)
			return nil, errors.New(msg)
		}
		if sslSvc != serviceName {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.annotations nginx.org/ssl-services: %v does not match backend.serviceName: %v\n", ingName, ingNamespace, sslSvc, serviceName)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// reject minion ingress if it is missing required lables
		reqLabel, reqLabelValue, ok := checkMinionRequiredLabels(&ingMetaData)
		if !ok {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.labels.%v: %v is missing or invalid\n", ingName, ingNamespace, reqLabel, reqLabelValue)
			log.Print(msg)
			return nil, errors.New(msg)
		}

		// try to get svc label if its missing add it.  If its present validate it matches serviceName
		svcLabelValue, ok := ingMetaData.Labels["svc"]
		if !ok {
			// svc label is missing, lets patch it into the ingress resource
			log.Printf("ingress name: %v namespace: %v is missing svc label, adding svc: %v to ingress", ingName, ingNamespace, serviceName)
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/metadata/labels/svc",
				Value: serviceName,
			})
		} else {
			if svcLabelValue != serviceName {
				msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. metadata.labels.svc: %v is invalid, it must match backend serviceName: %v\n", ingName, ingNamespace, svcLabelValue, serviceName)
				log.Print(msg)
				return nil, errors.New(msg)
			}
		}

		// enforce ingress name equal to serviceName or serviceName + "-inetsvcs"
		if serviceName == ingName || serviceName+"-inetsvcs" == ingName {
			if serviceName+"-inetsvcs" == ingName && !strings.Contains(ingress.Spec.Rules[0].Host, "inetsvcs") {
				msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. Ingress name can only contain inetsvcs if hostname contains inetsvcs, host:  %v\n", ingName, ingNamespace, ingress.Spec.Rules[0].Host)
				log.Print(msg)
				return nil, errors.New(msg)
			}
		} else {
			msg = fmt.Sprintf("Rejected ingress name: %v namespace: %v. Ingress name must be either %v or %v-inetsvcs\n", ingName, ingNamespace, serviceName, serviceName)
			log.Print(msg)
			return nil, errors.New(msg)

		}
	}

	return patches, nil
}
