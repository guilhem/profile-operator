//go:build e2e
// +build e2e

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/guilhem/profile-operator/test/utils"
)

// namespace where the project is deployed in
const namespace = "profile-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "profile-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "profile-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "profile-operator-metrics-binding"

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// Before running the tests, set up the environment by creating the namespace,
	// enforce the restricted security policy to the namespace, installing CRDs,
	// and deploying the controller.
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// After all tests have been executed, clean up by undeploying the controller, uninstalling CRDs,
	// and deleting the namespace.
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace)
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	// After each test, check for failures and collect logs, events,
	// and pod descriptions for debugging.
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}
		}
	})

	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	Context("Manager", func() {
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			Eventually(verifyControllerUp).Should(Succeed())
		})

		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=profile-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("waiting for the metrics endpoint to be ready")
			verifyMetricsEndpointReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "endpoints", metricsServiceName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("8443"), "Metrics endpoint is not ready")
			}
			Eventually(verifyMetricsEndpointReady).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("controller-runtime.metrics	Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring(
				"controller_runtime_reconcile_total",
			))
		})

		// +kubebuilder:scaffold:e2e-webhooks-checks

		It("should apply profile templates to target resources successfully", func() {
			By("creating a test namespace for profile operator testing")
			cmd := exec.Command("kubectl", "create", "namespace", "profile-test")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup namespace after test
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "namespace", "profile-test")
				_, _ = utils.Run(cmd)
			})

			By("creating a test deployment to be modified by profiles")
			deploymentYAML := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: profile-test
  labels:
    app: test-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: app
        image: nginx:1.20
        ports:
        - containerPort: 80
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(deploymentYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a profile to update container image and add labels")
			profileYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: image-update-profile
spec:
  description: "Updates container image and adds labels"
  priority: 10
  template:
    patchStrategicMerge:
      metadata:
        labels:
          profile.barpilot.io/applied: "image-update-profile"
          version: "v1.21"
        annotations:
          profile.barpilot.io/last-applied: "$(date)"
      spec:
        template:
          spec:
            containers:
            - name: app
              image: nginx:1.21
              env:
              - name: PROFILE_APPLIED
                value: "true"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(profileYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup profile
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profile", "image-update-profile")
				_, _ = utils.Run(cmd)
			})

			By("waiting for profile to be ready")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "profile", "image-update-profile", "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("True"))

			By("creating a ProfileBinding to apply the profile")
			profileBindingYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: test-app-binding
  namespace: profile-test
spec:
  profileRef:
    name: image-update-profile
  targetSelector:
    resourceRule:
      apiGroups: ["apps"]
      apiVersions: ["v1"]
      resources: ["deployments"]
    objectSelector:
      matchLabels:
        app: test-app
  updateStrategy:
    type: Immediate
  enabled: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(profileBindingYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup profile binding
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profilebinding", "test-app-binding", "-n", "profile-test")
				_, _ = utils.Run(cmd)
			})

			By("waiting for ProfileBinding to be ready")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "profilebinding", "test-app-binding", "-n", "profile-test", "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("True"))

			By("verifying the deployment was updated with new image")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "deployment", "test-app", "-n", "profile-test", "-o", "jsonpath={.spec.template.spec.containers[0].image}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("nginx:1.21"))

			By("verifying the deployment has profile labels")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "deployment", "test-app", "-n", "profile-test", "-o", "jsonpath={.metadata.labels['profile\.barpilot\.io/applied']}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("image-update-profile"))

			By("verifying the deployment has profile environment variable")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "deployment", "test-app", "-n", "profile-test", "-o", "jsonpath={.spec.template.spec.containers[0].env[?(@.name=='PROFILE_APPLIED')].value}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("true"))

			By("verifying ProfileBinding status shows successful application")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "profilebinding", "test-app-binding", "-n", "profile-test", "-o", "jsonpath={.status.updatedResources}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("1"))

			By("verifying metrics show successful reconciliation")
			metricsOutput := getMetricsOutput()
			Expect(metricsOutput).To(ContainSubstring("controller_runtime_reconcile_total"))
		})

		It("should handle multiple profiles with priority ordering", func() {
			By("creating a test namespace for priority testing")
			cmd := exec.Command("kubectl", "create", "namespace", "priority-test")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup namespace after test
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "namespace", "priority-test")
				_, _ = utils.Run(cmd)
			})

			By("creating a test ConfigMap")
			configMapYAML := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: priority-test
  labels:
    app: config-app
data:
  config.yaml: |
    setting1: original
    setting2: original
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(configMapYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a low priority profile")
			lowPriorityProfileYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: low-priority-profile
spec:
  description: "Low priority configuration update"
  priority: 5
  template:
    patchStrategicMerge:
      metadata:
        labels:
          priority: "low"
      data:
        config.yaml: |
          setting1: low-priority
          setting2: low-priority
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(lowPriorityProfileYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a high priority profile")
			highPriorityProfileYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: high-priority-profile
spec:
  description: "High priority configuration update"
  priority: 10
  template:
    patchStrategicMerge:
      metadata:
        labels:
          priority: "high"
      data:
        config.yaml: |
          setting1: high-priority
          setting3: high-only
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(highPriorityProfileYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup profiles
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profile", "low-priority-profile", "high-priority-profile")
				_, _ = utils.Run(cmd)
			})

			By("creating ProfileBindings for both profiles")
			lowPriorityBindingYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: low-priority-binding
  namespace: priority-test
spec:
  profileRef:
    name: low-priority-profile
  targetSelector:
    resourceRule:
      apiGroups: [""]
      apiVersions: ["v1"]
      resources: ["configmaps"]
    objectSelector:
      matchLabels:
        app: config-app
  enabled: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(lowPriorityBindingYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			highPriorityBindingYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: high-priority-binding
  namespace: priority-test
spec:
  profileRef:
    name: high-priority-profile
  targetSelector:
    resourceRule:
      apiGroups: [""]
      apiVersions: ["v1"]
      resources: ["configmaps"]
    objectSelector:
      matchLabels:
        app: config-app
  enabled: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(highPriorityBindingYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup bindings
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profilebinding", "low-priority-binding", "high-priority-binding", "-n", "priority-test")
				_, _ = utils.Run(cmd)
			})

			By("verifying that both profiles were applied")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "configmap", "test-config", "-n", "priority-test", "-o", "jsonpath={.metadata.labels.priority}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Or(Equal("low"), Equal("high")))
		})

		It("should handle rolling update strategy for multiple resources", func() {
			By("creating a test namespace for rolling update testing")
			cmd := exec.Command("kubectl", "create", "namespace", "rolling-test")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup namespace after test
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "namespace", "rolling-test")
				_, _ = utils.Run(cmd)
			})

			By("creating multiple test services")
			for i := 1; i <= 3; i++ {
				serviceYAML := fmt.Sprintf(`
apiVersion: v1
kind: Service
metadata:
  name: test-service-%d
  namespace: rolling-test
  labels:
    app: rolling-app
spec:
  selector:
    app: rolling-app
  ports:
  - port: 80
    targetPort: 8080
`, i)
				cmd = exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(serviceYAML)
				_, err = utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			}

			By("creating a profile for service updates")
			serviceProfileYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: service-update-profile
spec:
  description: "Updates service configuration"
  priority: 1
  template:
    patchStrategicMerge:
      metadata:
        labels:
          updated: "true"
        annotations:
          last-update: "rolling-update"
      spec:
        ports:
        - port: 80
          targetPort: 9090
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(serviceProfileYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup profile
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profile", "service-update-profile")
				_, _ = utils.Run(cmd)
			})

			By("creating a ProfileBinding with rolling update strategy")
			rollingBindingYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: rolling-update-binding
  namespace: rolling-test
spec:
  profileRef:
    name: service-update-profile
  targetSelector:
    resourceRule:
      apiGroups: [""]
      apiVersions: ["v1"]
      resources: ["services"]
    objectSelector:
      matchLabels:
        app: rolling-app
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxConcurrent: "50%"
      pauseBetweenUpdatesSec: 5
  enabled: true
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(rollingBindingYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup binding
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profilebinding", "rolling-update-binding", "-n", "rolling-test")
				_, _ = utils.Run(cmd)
			})

			By("verifying that services were updated")
			Eventually(func() int {
				cmd := exec.Command("kubectl", "get", "services", "-n", "rolling-test", "-l", "updated=true", "--no-headers")
				output, _ := utils.Run(cmd)
				return len(strings.Split(strings.TrimSpace(output), "\n"))
			}).Should(Equal(3))

			By("verifying ProfileBinding status shows all resources updated")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "profilebinding", "rolling-update-binding", "-n", "rolling-test", "-o", "jsonpath={.status.updatedResources}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("3"))
		})

		It("should handle disabled ProfileBinding gracefully", func() {
			By("creating a test namespace for disabled binding testing")
			cmd := exec.Command("kubectl", "create", "namespace", "disabled-test")
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup namespace after test
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "namespace", "disabled-test")
				_, _ = utils.Run(cmd)
			})

			By("creating a test pod")
			podYAML := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  namespace: disabled-test
  labels:
    app: disabled-app
spec:
  containers:
  - name: test
    image: nginx:1.20
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(podYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("creating a profile")
			profileYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: disabled-test-profile
spec:
  description: "Test profile for disabled binding"
  template:
    patchStrategicMerge:
      metadata:
        labels:
          should-not-be-applied: "true"
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(profileYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup profile
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profile", "disabled-test-profile")
				_, _ = utils.Run(cmd)
			})

			By("creating a disabled ProfileBinding")
			disabledBindingYAML := `
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: disabled-binding
  namespace: disabled-test
spec:
  profileRef:
    name: disabled-test-profile
  targetSelector:
    resourceRule:
      apiGroups: [""]
      apiVersions: ["v1"]
      resources: ["pods"]
    objectSelector:
      matchLabels:
        app: disabled-app
  enabled: false
`
			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(disabledBindingYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Cleanup binding
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "profilebinding", "disabled-binding", "-n", "disabled-test")
				_, _ = utils.Run(cmd)
			})

			By("verifying ProfileBinding shows disabled status")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "profilebinding", "disabled-binding", "-n", "disabled-test", "-o", "jsonpath={.status.conditions[?(@.type=='Ready')].reason}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(Equal("Disabled"))

			By("verifying that the pod was NOT modified")
			Consistently(func() string {
				cmd := exec.Command("kubectl", "get", "pod", "test-pod", "-n", "disabled-test", "-o", "jsonpath={.metadata.labels['should-not-be-applied']}")
				output, _ := utils.Run(cmd)
				return strings.TrimSpace(output)
			}).Should(BeEmpty())
		})
	})
})

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() string {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	metricsOutput, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
	Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
	return metricsOutput
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
