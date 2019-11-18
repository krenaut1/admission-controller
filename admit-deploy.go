package main

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"k8s.io/api/admission/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	deployAppsResource = metav1.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// admitDeploy
func admitDeploy(req *v1beta1.AdmissionRequest) ([]patchOperation, error) {
	var msg string
	// This handler should only get called on ingress objects as per the MutatingWebhookConfiguration in the YAML file.
	// However, if (for whatever reason) this gets invoked on an object of a different kind, issue a log message but
	// let the object request pass through otherwise.
	msg = fmt.Sprintf("admitDeploy evoked! Namespace: %v Name: %v Group: %v Version: %v Resource: %v Operation: %v\n", req.Namespace, req.Name, req.Resource.Group, req.Resource.Version, req.Resource.Resource, req.Operation)
	log.Print(msg)
	raw := req.Object.Raw
	logReq(raw)

	// approve any deployment that is in an exempt Namespace
	if !namespaceIsMonitored(req.Namespace) {
		log.Printf("Approved deployment name: %v namespace: %v. Namespace is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}

	// approve any ingress that is specifically exempt
	if deployIsExempt(req.Namespace, req.Name) {
		log.Printf("Approved deployment name: %v namespace: %v. deployment is exempt from webhook validation\n", req.Name, req.Namespace)
		return nil, nil
	}

	// Parse the deployment object.
	deploy := appsv1.Deployment{}
	if _, _, err := universalDeserializer.Decode(raw, nil, &deploy); err != nil {
		return nil, fmt.Errorf("could not deserialize deployment object: %v, deployment is being rejected", err)
	}

	// Retrieve the name and namespace
	deployMetaData := deploy.ObjectMeta
	deployName := deployMetaData.Name
	deployNamespace := deployMetaData.Namespace

	log.Printf("Validating deployment name: %v namespace: %v\n", deployName, deployNamespace)

	// reject if annotations section is missing
	if deployMetaData.Annotations == nil {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. metadata.annotations object is missing\n", deployName, deployNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if description is missing
	_, ok := deployMetaData.Annotations["description"]
	if !ok {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. metadata.annotations.description is missing\n", deployName, deployNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if svc label is not present
	svcLabelValue, ok := deployMetaData.Labels["svc"]
	if !ok {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. metadata.labels.svc is missing\n", deployName, deployNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if deployment name not equal to svc label
	if deployName != svcLabelValue {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. metadata.labels.svc: %v must be equal to deployment name\n", deployName, deployNamespace, svcLabelValue)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if template svc label is not present
	templateSvcLabelValue, ok := deploy.Spec.Template.ObjectMeta.Labels["svc"]
	if !ok {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. spec.template.metadata.labels.svc is missing\n", deployName, deployNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if template svc label not equal to metadata svc label
	if svcLabelValue != templateSvcLabelValue {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. spec.template.metadata.labels.svc: %v must be equal to metadata.lables.svc: %v\n", deployName, deployNamespace, templateSvcLabelValue, svcLabelValue)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if matchlabels svc label is not present
	matchSvcLabelValue, ok := deploy.Spec.Selector.MatchLabels["svc"]
	if !ok {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. spec.selector.matchlabels.svc is missing\n", deployName, deployNamespace)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// reject if template svc label not equal to metadata svc label
	if svcLabelValue != matchSvcLabelValue {
		msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. spec.selector.matchlabels.svc: %v must be equal to metadata.lables.svc: %v\n", deployName, deployNamespace, matchSvcLabelValue, svcLabelValue)
		log.Print(msg)
		return nil, errors.New(msg)
	}

	// declare patchOperation array as we may need to mutate this deployment
	var patches []patchOperation
	// get the array of containers for this deployment
	deployContainers := deploy.Spec.Template.Spec.Containers

	// create a container array to hold the mutated containers
	var newContainers []v1.Container

	// loop over the containers and mutate the resources limits and request
	for _, container := range deployContainers {
		// reject deployment if image name uses latest or stable tags
		imageName := strings.ToLower(container.Image)
		if strings.Contains(imageName, "latest") || strings.Contains(imageName, "stable") {
			msg = fmt.Sprintf("Rejected deployment name: %v namespace: %v. container image tag: %v must not contain latest or stable, use specific version tag\n", deployName, deployNamespace, imageName)
			log.Print(msg)
			return nil, errors.New(msg)
		}
		// mutate requests.cpu and requests.memory for this container
		updtResources(&container)
		// mutate env, add TZ="UTC" environment variable if not already set
		updtEnv(&container)
		// append the mutated container to the newContainers array
		newContainers = append(newContainers, container)
	}

	patches = append(patches, patchOperation{
		Op:   "replace",
		Path: "/spec/template/spec/containers",
		// replace the current container array with the mutated container array
		Value: newContainers,
	})

	// Retrieve the `runAsNonRoot` and `runAsUser` values.
	var runAsNonRoot *bool
	var runAsUser *int64
	if deploy.Spec.Template.Spec.SecurityContext != nil {
		runAsNonRoot = deploy.Spec.Template.Spec.SecurityContext.RunAsNonRoot
		runAsUser = deploy.Spec.Template.Spec.SecurityContext.RunAsUser
	}

	if runAsNonRoot == nil {
		patches = append(patches, patchOperation{
			Op:   "add",
			Path: "/spec/template/spec/securityContext/runAsNonRoot",
			// The value must not be true if runAsUser is set to 0, as otherwise we would create a conflicting
			// configuration ourselves.
			Value: runAsUser == nil || *runAsUser != 0,
		})

		if runAsUser == nil {
			patches = append(patches, patchOperation{
				Op:    "add",
				Path:  "/spec/template/spec/securityContext/runAsUser",
				Value: 65534,
			})
		}
	} else if *runAsNonRoot == true && (runAsUser != nil && *runAsUser == 0) {
		// Make sure that the settings are not contradictory, and fail the object creation if they are.
		return nil, errors.New("runAsNonRoot specified, but runAsUser set to 0 (the root user)")
	}

	log.Println("===== Begin Deployment Patch =====")
	for _, patch := range patches {
		log.Println(patch)
	}
	log.Println("===== End Deployment Patch =====")

	return patches, nil
}

func updtResources(c *v1.Container) {
	res := make(v1.ResourceList)
	res["cpu"] = resource.MustParse("1m")
	res["memory"] = resource.MustParse("8Mi")
	c.Resources.Requests = res
	return
}

func updtEnv(c *v1.Container) {
	// assume time zone env variable is not found
	var tzFound bool = false
	// create env variable structure to append to container env array if it is missing
	tzenv := v1.EnvVar{Name: "TZ", Value: "UTC"}
	// loop over all env variables looking for TZ variable
	for _, env := range c.Env {
		if env.Name == "TZ" {
			// indicate that we found a time zone env variable
			tzFound = true
		}
	}
	// if we did not find a time zone env variable then append TZ="UTC" to the container environment variables
	if !tzFound {
		c.Env = append(c.Env, tzenv)
	}
	return
}
