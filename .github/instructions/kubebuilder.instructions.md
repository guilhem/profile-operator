---
applyTo: "**"
---

# Kubebuilder Best Practices Guide

This document outlines the best practices for developing Kubernetes operators using Kubebuilder framework.

## Project Initialization

### Create a New Kubebuilder Project

Initialize a new Kubebuilder project:

```bash
# Create a new directory for your project
mkdir my-operator
cd my-operator

# Initialize the project
kubebuilder init --domain example.com --repo github.com/example/my-operator

# Initialize with specific Go version (optional)
kubebuilder init --domain example.com --repo github.com/example/my-operator --plugins go/v4
```

### Create APIs and Controllers

#### Create a New API and Controller

```bash
# Create a new API with controller
kubebuilder create api --group myapp --version v1 --kind MyResource

# Create API only (without controller)
kubebuilder create api --group myapp --version v1 --kind MyResource --controller=false

# Create controller only (without API)
kubebuilder create api --group myapp --version v1 --kind MyResource --resource=false
```

#### Create Multiple APIs

```bash
# Create different APIs for the same operator
kubebuilder create api --group myapp --version v1 --kind Database
kubebuilder create api --group myapp --version v1 --kind Application
kubebuilder create api --group myapp --version v1 --kind Config

# Create APIs with different versions
kubebuilder create api --group myapp --version v1alpha1 --kind MyResource
kubebuilder create api --group myapp --version v1beta1 --kind MyResource
```

### Create Webhooks

#### Admission Webhooks

```bash
# Create admission webhook (defaulting and validation)
kubebuilder create webhook --group myapp --version v1 --kind MyResource --defaulting --programmatic-validation

# Create validation webhook only
kubebuilder create webhook --group myapp --version v1 --kind MyResource --programmatic-validation

# Create defaulting webhook only
kubebuilder create webhook --group myapp --version v1 --kind MyResource --defaulting
```

#### Conversion Webhooks

```bash
# Create conversion webhook for API version conversion
kubebuilder create webhook --group myapp --version v1 --kind MyResource --conversion
```

### Generate Code and Manifests

```bash
# Generate deep copy methods
make generate

# Generate CRD manifests
make manifests

# Generate and update both
make generate manifests

# Install CRDs into the cluster
# ⚠️ WARNING: Only for production deployment, not for testing!
# Use envtest for all development and testing
kubectl config current-context  # VERIFY PRODUCTION CLUSTER!
make install

# Deploy the operator
# ⚠️ WARNING: Only for production deployment!
kubectl config current-context  # VERIFY PRODUCTION CLUSTER!
make deploy

# Undeploy the operator
make undeploy
```

## Project Structure

### Follow Standard Layout
- Keep the standard Kubebuilder project structure
- Use appropriate directories for different components:
  - `api/` - API definitions and types
  - `controllers/` - Controller logic
  - `config/` - Kubernetes manifests and configurations
  - `hack/` - Scripts and utilities
  - `test/` - Test files

### Organize Code Logically
```
├── api/v1/
│   ├── types.go
│   ├── zz_generated.deepcopy.go
│   └── groupversion_info.go
├── controllers/
│   ├── controller.go
│   └── suite_test.go
├── config/
│   ├── crd/
│   ├── rbac/
│   ├── manager/
│   └── samples/
```

## API Design

### Custom Resource Definitions (CRDs)

#### Use Semantic Versioning
- Start with `v1alpha1` for experimental APIs
- Progress to `v1beta1` for stable APIs
- Use `v1` for production-ready APIs

#### Define Clear Schemas
```go
type MyResourceSpec struct {
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`
    
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=100
    Replicas *int32 `json:"replicas,omitempty"`
    
    // +kubebuilder:default="development"
    // +kubebuilder:validation:Enum=development;staging;production
    Environment string `json:"environment,omitempty"`
}
```

#### Status Subresource
Always implement a status subresource for tracking resource state using **conditions-first approach**:

```go
type MyResourceStatus struct {
    // Conditions represent the latest available observations of the resource state
    // This is the PRIMARY way to track status in modern Kubernetes APIs
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    
    // Phase represents the current phase of the resource (computed from conditions)
    // DEPRECATED: Use conditions for new APIs, keep phase only for backward compatibility
    // +optional
    Phase string `json:"phase,omitempty"`
    
    // ObservedGeneration reflects the generation most recently observed
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    
    // Message provides human-readable details about the resource state
    // +optional
    Message string `json:"message,omitempty"`
    
    // LastUpdated indicates when the status was last updated
    // +optional
    LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}```

**Condition Types Best Practices:**
- Use standardized condition types: `Ready`, `Available`, `Progressing`, `Degraded`
- Always provide meaningful `Reason` and `Message` fields
- Let `meta.SetStatusCondition` handle `LastTransitionTime` automatically

#### Use Kubebuilder Markers
- `+kubebuilder:validation:*` - For field validation
- `+kubebuilder:default:*` - For default values
- `+kubebuilder:printcolumn:*` - For kubectl output customization
- `+kubebuilder:subresource:status` - Enable status subresource

## Controller Implementation

### Reconcile Function Best Practices

#### Idempotent Operations
Ensure all operations are idempotent - running multiple times should produce the same result:

```go
func (r *MyResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // Fetch the resource
    var resource myv1.MyResource
    if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil // Resource deleted
        }
        return ctrl.Result{}, err
    }
    
    // Always update status at the end
    defer func() {
        if err := r.Status().Update(ctx, &resource); err != nil {
            log.Error(err, "Failed to update status")
        }
    }()
    
    // Implement reconciliation logic
    return r.reconcileResource(ctx, &resource)
}
```

#### Use Finalizers for Cleanup
```go
const myResourceFinalizer = "myresource.example.com/finalizer"

func (r *MyResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := logf.FromContext(ctx)
    
    // Fetch the resource
    var resource myv1.MyResource
    if err := r.Get(ctx, req.NamespacedName, &resource); err != nil {
        if apierrors.IsNotFound(err) {
            return ctrl.Result{}, nil // Resource deleted
        }
        return ctrl.Result{}, err
    }
    
    // Handle deletion vs normal reconciliation
    if !resource.ObjectMeta.DeletionTimestamp.IsZero() {
        return r.reconcileDelete(ctx, &resource)
    }
    
    return r.reconcileNormal(ctx, &resource)
}

func (r *MyResourceReconciler) reconcileNormal(ctx context.Context, resource *myv1.MyResource) (ctrl.Result, error) {
    // Add finalizer if not present using CreateOrPatch pattern
    if !controllerutil.ContainsFinalizer(resource, myResourceFinalizer) {
        op, err := controllerutil.CreateOrPatch(ctx, r.Client, resource, func() error {
            controllerutil.AddFinalizer(resource, myResourceFinalizer)
            return nil
        })
        if err != nil {
            r.Recorder.Event(resource, corev1.EventTypeWarning, "FinalizerFailed", 
                fmt.Sprintf("Failed to add finalizer: %v", err))
            return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
        }
        if op != controllerutil.OperationResultNone {
            r.Recorder.Event(resource, corev1.EventTypeNormal, "FinalizerAdded", 
                "Finalizer added successfully")
        }
        return ctrl.Result{RequeueAfter: time.Second * 2}, nil
    }
    
    // Normal reconciliation logic
    return r.processResource(ctx, resource)
}

func (r *MyResourceReconciler) reconcileDelete(ctx context.Context, resource *myv1.MyResource) (ctrl.Result, error) {
    log := logf.FromContext(ctx)
    
    // Check if our finalizer is present
    if !controllerutil.ContainsFinalizer(resource, myResourceFinalizer) {
        return ctrl.Result{}, nil // Nothing to clean up
    }
    
    log.Info("Performing cleanup for resource deletion", "name", resource.Name)
    
    // Perform cleanup tasks
    if err := r.cleanupResources(ctx, resource); err != nil {
        log.Error(err, "Failed to cleanup resources")
        return ctrl.Result{}, err
    }
    
    // Remove finalizer using CreateOrPatch pattern
    op, err := controllerutil.CreateOrPatch(ctx, r.Client, resource, func() error {
        controllerutil.RemoveFinalizer(resource, myResourceFinalizer)
        return nil
    })
    if err != nil {
        log.Error(err, "Failed to remove finalizer")
        r.Recorder.Event(resource, corev1.EventTypeWarning, "FinalizerRemovalFailed", 
            fmt.Sprintf("Failed to remove finalizer: %v", err))
        return ctrl.Result{}, err
    }
    if op != controllerutil.OperationResultNone {
        r.Recorder.Event(resource, corev1.EventTypeNormal, "FinalizerRemoved", 
            "Finalizer removed successfully")
    }
    
    log.Info("Successfully completed resource deletion", "name", resource.Name)
    return ctrl.Result{}, nil
}```

#### Error Handling and Requeue
```go
func (r *MyResourceReconciler) reconcileNormal(ctx context.Context, resource *myv1.MyResource) (ctrl.Result, error) {
    // Temporary errors - requeue with backoff
    if err := r.createDependentResource(ctx, resource); err != nil {
        if apierrors.IsConflict(err) || apierrors.IsServerTimeout(err) {
            return ctrl.Result{RequeueAfter: time.Second * 30}, nil
        }
        return ctrl.Result{}, err
    }
    
    // Success - requeue after longer interval for status checks
    return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}
```

### Use Controller Runtime Utilities
- `controllerutil.SetControllerReference()` - Set owner references
- `controllerutil.CreateOrPatch()` - Idempotent resource creation/updates (preferred over CreateOrUpdate)
- `controllerutil.ContainsFinalizer()` / `AddFinalizer()` / `RemoveFinalizer()`

### Event Management with CreateOrPatch Pattern

#### EventRecorder Integration
Always include EventRecorder in your controller struct and inject it during setup:

```go
type MyResourceReconciler struct {
    client.Client
    Scheme   *runtime.Scheme
    Recorder record.EventRecorder  // Add EventRecorder field
}

// In main.go, inject EventRecorder
if err = (&controller.MyResourceReconciler{
    Client:   mgr.GetClient(),
    Scheme:   mgr.GetScheme(),
    Recorder: mgr.GetEventRecorderFor("myresource-controller"),
}).SetupWithManager(mgr); err != nil {
    setupLog.Error(err, "unable to create controller", "controller", "MyResource")
    os.Exit(1)
}
```

#### CreateOrPatch with Event Generation
Use `controllerutil.CreateOrPatch` with operation-based event generation:

```go
func (r *MyResourceReconciler) updateResource(ctx context.Context, resource *myv1.MyResource) error {
    op, err := controllerutil.CreateOrPatch(ctx, r.Client, resource, func() error {
        // Make modifications within the mutation function
        controllerutil.AddFinalizer(resource, myResourceFinalizer)
        // Any other modifications go here
        return nil
    })
    
    // Generate events based on operation result
    if err != nil {
        r.Recorder.Event(resource, corev1.EventTypeWarning, "UpdateFailed", 
            fmt.Sprintf("Failed to update resource: %v", err))
        return err
    }
    
    // Generate events for successful operations
    switch op {
    case controllerutil.OperationResultCreated:
        r.Recorder.Event(resource, corev1.EventTypeNormal, "Created", 
            "Resource created successfully")
    case controllerutil.OperationResultUpdated:
        r.Recorder.Event(resource, corev1.EventTypeNormal, "Updated", 
            "Resource updated successfully")
    case controllerutil.OperationResultNone:
        // No event needed - resource was already up to date
    }
    
    return nil
}
```

#### Advanced CreateOrPatch Patterns for Complex Resources

For complex resource management like applying profile templates:

```go
func (r *ProfileBindingReconciler) applyProfileToResource(ctx context.Context, 
    binding *v1alpha1.ProfileBinding, profile *v1alpha1.Profile, 
    target *unstructured.Unstructured) error {
    
    // Create a copy for modification
    updated := target.DeepCopy()
    
    // Apply profile template to the copy
    if err := r.applyTemplate(profile, updated); err != nil {
        return fmt.Errorf("failed to apply template: %w", err)
    }
    
    // Check if changes are needed
    if !r.hasResourceChanges(target, updated) {
        return nil // No changes needed
    }
    
    // Apply changes using CreateOrPatch
    op, err := controllerutil.CreateOrPatch(ctx, r.Client, target, func() error {
        // Apply all modifications within the mutation function
        target.Object = updated.Object
        return nil
    })
    
    if err != nil {
        r.Recorder.Eventf(binding, corev1.EventTypeWarning, "ProfileApplicationFailed",
            "Failed to apply profile %s to %s/%s: %v", 
            profile.Name, target.GetKind(), target.GetName(), err)
        return err
    }
    
    // Generate appropriate events
    switch op {
    case controllerutil.OperationResultUpdated:
        r.Recorder.Eventf(binding, corev1.EventTypeNormal, "ProfileApplied",
            "Successfully applied profile %s to %s/%s", 
            profile.Name, target.GetKind(), target.GetName())
    case controllerutil.OperationResultNone:
        // Resource already up to date
    }
    
    return nil
}
```

#### RBAC for Events
Don't forget to add event permissions to your controller's RBAC:

```go
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

### Watches and Predicates
```go
func (r *MyResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&myv1.MyResource{}).
        Owns(&appsv1.Deployment{}).
        Watches(
            &source.Kind{Type: &corev1.ConfigMap{}},
            handler.EnqueueRequestsFromMapFunc(r.findResourcesForConfigMap),
            builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
        ).
        Complete(r)
}
```

## Webhooks

### Admission Webhooks

Kubebuilder supports three types of admission webhooks:

#### Defaulting Webhooks
Automatically set default values for fields:

```go
// +kubebuilder:webhook:path=/mutate-myapp-example-com-v1-myresource,mutating=true,failurePolicy=fail,sideEffects=None,groups=myapp.example.com,resources=myresources,verbs=create;update,versions=v1,name=mmyresource.kb.io,admissionReviewVersions=v1

func (r *MyResource) Default() {
    myresourcelog.Info("default", "name", r.Name)
    
    // Set default values
    if r.Spec.Replicas == nil {
        r.Spec.Replicas = &[]int32{1}[0]
    }
    
    if r.Spec.Environment == "" {
        r.Spec.Environment = "development"
    }
}
```

#### Validation Webhooks
Validate resource specifications:

```go
// +kubebuilder:webhook:path=/validate-myapp-example-com-v1-myresource,mutating=false,failurePolicy=fail,sideEffects=None,groups=myapp.example.com,resources=myresources,verbs=create;update,versions=v1,name=vmyresource.kb.io,admissionReviewVersions=v1

func (r *MyResource) ValidateCreate() error {
    myresourcelog.Info("validate create", "name", r.Name)
    return r.validateMyResource()
}

func (r *MyResource) ValidateUpdate(old runtime.Object) error {
    myresourcelog.Info("validate update", "name", r.Name)
    return r.validateMyResource()
}

func (r *MyResource) ValidateDelete() error {
    myresourcelog.Info("validate delete", "name", r.Name)
    return nil
}

func (r *MyResource) validateMyResource() error {
    var allErrs field.ErrorList
    
    if r.Spec.Replicas != nil && *r.Spec.Replicas < 0 {
        allErrs = append(allErrs, field.Invalid(
            field.NewPath("spec").Child("replicas"),
            *r.Spec.Replicas,
            "replicas must be non-negative",
        ))
    }
    
    if len(allErrs) == 0 {
        return nil
    }
    
    return apierrors.NewInvalid(
        schema.GroupKind{Group: "myapp.example.com", Kind: "MyResource"},
        r.Name, allErrs)
}
```

#### Conversion Webhooks
Handle API version conversions:

```go
// +kubebuilder:webhook:path=/convert,mutating=false,failurePolicy=fail,sideEffects=None,groups=myapp.example.com,resources=myresources,verbs=create;update,versions=v1;v1beta1,name=cmyresource.kb.io,admissionReviewVersions=v1

func (src *MyResource) ConvertTo(dstRaw conversion.Hub) error {
    dst := dstRaw.(*v1.MyResource)
    
    // Convert from current version to hub version
    dst.ObjectMeta = src.ObjectMeta
    dst.Spec.Name = src.Spec.Name
    // Handle field conversions...
    
    return nil
}

func (dst *MyResource) ConvertFrom(srcRaw conversion.Hub) error {
    src := srcRaw.(*v1.MyResource)
    
    // Convert from hub version to current version
    dst.ObjectMeta = src.ObjectMeta
    dst.Spec.Name = src.Spec.Name
    // Handle field conversions...
    
    return nil
}
```

### Webhook Development Commands

```bash
# Enable webhooks in your project
kubebuilder create webhook --group myapp --version v1 --kind MyResource --defaulting --programmatic-validation

# Test webhooks locally
make run ENABLE_WEBHOOKS=false  # Run without webhooks for testing
make run                        # Run with webhooks enabled

# Install cert-manager for webhook certificates
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Deploy with webhooks
make deploy
```

### Webhook Best Practices

1. **Idempotent Defaulting**: Ensure defaulting webhooks are idempotent
2. **Graceful Validation**: Provide clear error messages
3. **Performance**: Keep webhook logic fast and lightweight
4. **Failure Policy**: Choose appropriate failure policies
5. **Certificate Management**: Use cert-manager for webhook certificates

## Error Handling

### Use Appropriate Error Types
```go
import (
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    "k8s.io/apimachinery/pkg/api/meta"
)

// Check for specific error types
if apierrors.IsNotFound(err) {
    // Handle not found
}
if apierrors.IsConflict(err) {
    // Handle conflicts (retry)
}
if meta.IsNoMatchError(err) {
    // Handle API not available
}
```

### Modern Status Management with Conditions

#### Prefer Conditions Over Phases

**✅ DO: Use conditions as the primary status mechanism**
```go
// Standard condition types for most resources
const (
    ConditionReady      = "Ready"      // Resource is ready for use
    ConditionAvailable  = "Available"  // Resource is available/accessible
    ConditionProgressing = "Progressing" // Resource is being processed
    ConditionDegraded   = "Degraded"   // Resource has issues but is functional
)

// Standard condition reasons
const (
    ReasonPending     = "Pending"
    ReasonSucceeded   = "Succeeded"
    ReasonFailed      = "Failed"
    ReasonApplying    = "Applying"
    ReasonHealthy     = "Healthy"
)
```

**❌ AVOID: Using phases as primary status**
```go
// Legacy approach - avoid for new APIs
type MyResourceStatus struct {
    Phase string `json:"phase,omitempty"` // "Pending", "Running", "Failed"
}
```

#### Condition Management Helper Methods

```go
// initializeConditions sets up initial conditions
func (r *MyResourceReconciler) initializeConditions(resource *myv1.MyResource) {
    initialConditions := []metav1.Condition{
        {
            Type:    ConditionReady,
            Status:  metav1.ConditionFalse,
            Reason:  ReasonPending,
            Message: "Resource is being processed",
        },
        {
            Type:    ConditionProgressing,
            Status:  metav1.ConditionTrue,
            Reason:  ReasonApplying,
            Message: "Resource application in progress",
        },
    }
    
    for _, condition := range initialConditions {
        meta.SetStatusCondition(&resource.Status.Conditions, condition)
    }
}

// setCondition updates or creates a condition
func (r *MyResourceReconciler) setCondition(resource *myv1.MyResource, conditionType string, status metav1.ConditionStatus, reason, message string) {
    condition := metav1.Condition{
        Type:    conditionType,
        Status:  status,
        Reason:  reason,
        Message: message,
        // LastTransitionTime is handled automatically by meta.SetStatusCondition
    }
    
    meta.SetStatusCondition(&resource.Status.Conditions, condition)
}

// computePhaseFromConditions provides backward compatibility
func (r *MyResourceReconciler) computePhaseFromConditions(resource *myv1.MyResource) string {
    readyCondition := meta.FindStatusCondition(resource.Status.Conditions, ConditionReady)
    degradedCondition := meta.FindStatusCondition(resource.Status.Conditions, ConditionDegraded)
    
    if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
        return "Degraded"
    }
    
    if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
        return "Ready"
    }
    
    return "Pending"
}
```

#### Status Update Pattern

**Always fetch the latest resource version before status updates to avoid conflicts:**

```go
func (r *MyResourceReconciler) updateResourceStatus(ctx context.Context, resource *myv1.MyResource) {
    log := logf.FromContext(ctx)
    
    // Fetch the latest version to ensure correct resource version
    var latestResource myv1.MyResource
    if err := r.Get(ctx, types.NamespacedName{
        Name: resource.Name, 
        Namespace: resource.Namespace,
    }, &latestResource); err != nil {
        log.Error(err, "Failed to get latest resource for status update")
        return
    }
    
    // Copy status fields from working copy to latest version
    latestResource.Status = resource.Status
    
    // Compute phase from conditions for backward compatibility
    latestResource.Status.Phase = r.computePhaseFromConditions(&latestResource)
    latestResource.Status.LastUpdated = &metav1.Time{Time: time.Now()}
    
    if err := r.Status().Update(ctx, &latestResource); err != nil {
        log.Error(err, "Failed to update status")
    }
}

func (r *MyResourceReconciler) reconcileNormal(ctx context.Context, resource *myv1.MyResource) (ctrl.Result, error) {
    log := logf.FromContext(ctx)
    
    // Always update status at the end
    defer func() {
        r.updateResourceStatus(ctx, resource)
    }()
    
    // Initialize conditions on first reconciliation
    if len(resource.Status.Conditions) == 0 {
        r.initializeConditions(resource)
        resource.Status.ObservedGeneration = resource.Generation
        return ctrl.Result{RequeueAfter: time.Second * 2}, nil
    }
    
    // Reconciliation logic with condition updates
    if err := r.processResource(ctx, resource); err != nil {
        r.setCondition(resource, ConditionReady, metav1.ConditionFalse, ReasonFailed, err.Error())
        r.setCondition(resource, ConditionDegraded, metav1.ConditionTrue, ReasonFailed, "Processing failed")
        return ctrl.Result{}, err
    }
    
    // Success
    r.setCondition(resource, ConditionReady, metav1.ConditionTrue, ReasonSucceeded, "Resource is ready")
    r.setCondition(resource, ConditionDegraded, metav1.ConditionFalse, ReasonHealthy, "No issues detected")
    
    return ctrl.Result{}, nil
}
```

### Update Status Conditions
```go
import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/api/meta"
)

func (r *MyResourceReconciler) updateCondition(resource *myv1.MyResource, conditionType string, status metav1.ConditionStatus, reason, message string) {
    condition := metav1.Condition{
        Type:    conditionType,
        Status:  status,
        Reason:  reason,
        Message: message,
        // DO NOT set LastTransitionTime manually - meta.SetStatusCondition handles this optimally
    }
    
    meta.SetStatusCondition(&resource.Status.Conditions, condition)
}
```

## Testing

### Safe Testing Practices

**⚠️ IMPORTANT SAFETY GUIDELINES:**
- **ALWAYS** use `envtest` for testing - it provides a complete Kubernetes API without requiring a real cluster
- **NEVER** run `make install` or `make deploy` during development and testing
- **AVOID** connecting to real clusters for routine testing
- **USE** `envtest` for all unit, integration, and controller testing

### Recommended Testing Approach

**Use `envtest` exclusively for all testing scenarios:**
- Complete Kubernetes API simulation without real cluster
- Fast test execution
- No cluster configuration required
- Safe and isolated testing environment

### Local Testing with envtest (Exclusive Approach)

#### 1. Unit Tests with envtest
Use the controller-runtime testing framework - the **ONLY** recommended approach for testing:

```go
func TestMyResourceController(t *testing.T) {
    g := gomega.NewWithT(t)
    
    // Setup test environment - NO REAL CLUSTER NEEDED
    testEnv := &envtest.Environment{
        CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
        ErrorIfCRDPathMissing: true,
    }
    
    cfg, err := testEnv.Start()
    g.Expect(err).NotTo(gomega.HaveOccurred())
    defer testEnv.Stop()
    
    // Create test client
    k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
    g.Expect(err).NotTo(gomega.HaveOccurred())
    
    // Create fake event recorder for testing
    fakeRecorder := record.NewFakeRecorder(10)
    
    // Setup controller with fake recorder
    reconciler := &MyResourceReconciler{
        Client:   k8sClient,
        Scheme:   scheme.Scheme,
        Recorder: fakeRecorder,
    }
    
    // Test scenarios
    ctx := context.Background()
    
    // Test resource creation
    resource := &myv1.MyResource{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-resource",
            Namespace: "default",
        },
        Spec: myv1.MyResourceSpec{
            Name: "test",
        },
    }
    
    err = k8sClient.Create(ctx, resource)
    g.Expect(err).NotTo(gomega.HaveOccurred())
    
    // Test controller reconciliation
    req := ctrl.Request{
        NamespacedName: client.ObjectKeyFromObject(resource),
    }
    
    result, err := reconciler.Reconcile(ctx, req)
    g.Expect(err).NotTo(gomega.HaveOccurred())
    
    // Verify events were generated
    select {
    case event := <-fakeRecorder.Events:
        g.Expect(event).To(gomega.ContainSubstring("FinalizerAdded"))
    default:
        t.Fatal("Expected event was not generated")
    }
    
    // Test controller logic
    // ...
}
```

#### 2. Controller Testing with envtest
Test your controller completely with envtest:

```bash
# Generate manifests and code
make generate manifests

# Run all tests with envtest (no cluster needed)
make test

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

#### 3. Webhook Testing with envtest
Test webhooks using envtest environment:

```bash
# Webhooks are automatically available in envtest
# No additional setup required

# Test webhook functionality in your test suite
func TestWebhooks(t *testing.T) {
    // envtest automatically handles webhook testing
    // Test defaulting webhooks
    resource := &myv1.MyResource{}
    resource.Default() // Test defaulting logic
    
    // Test validation webhooks
    err := resource.ValidateCreate() // Test validation logic
    // Assert expected behavior
}

# Run webhook tests
go test -v ./api/... # Test webhook implementations
```

### Integration Tests

#### Setup Integration Test Environment

```go
// controllers/suite_test.go
package controllers

import (
    "context"
    "path/filepath"
    "testing"
    "time"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"
    "k8s.io/client-go/kubernetes/scheme"
    "k8s.io/client-go/rest"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/envtest"
    logf "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/log/zap"

    myv1 "github.com/example/my-operator/api/v1"
)

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment

func TestControllers(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
    logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

    By("bootstrapping test environment")
    testEnv = &envtest.Environment{
        CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
        ErrorIfCRDPathMissing: true,
    }

    var err error
    cfg, err = testEnv.Start()
    Expect(err).NotTo(HaveOccurred())
    Expect(cfg).NotTo(BeNil())

    err = myv1.AddToScheme(scheme.Scheme)
    Expect(err).NotTo(HaveOccurred())

    k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
    Expect(err).NotTo(HaveOccurred())
    Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
    By("tearing down the test environment")
    err := testEnv.Stop()
    Expect(err).NotTo(HaveOccurred())
})
```

#### Safe Integration Test Commands

```bash
# Run integration tests (uses envtest, no real cluster)
make test

# Run tests with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run specific test
go test -v ./controllers -run TestMyResourceController

# Run tests with race detection
go test -race ./...
```

### Advanced envtest Testing

#### Complete Controller Testing

```go
// Test complete controller reconciliation with envtest
func TestControllerReconciliation(t *testing.T) {
    ctx := context.Background()
    
    // Create test resource
    resource := &myv1.MyResource{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-resource",
            Namespace: "default",
        },
        Spec: myv1.MyResourceSpec{
            Replicas: ptr.To(int32(3)),
        },
    }
    
    // Test resource creation
    err := k8sClient.Create(ctx, resource)
    require.NoError(t, err)
    
    // Test controller reconciliation
    reconciler := &MyResourceReconciler{
        Client: k8sClient,
        Scheme: scheme.Scheme,
    }
    
    req := ctrl.Request{
        NamespacedName: client.ObjectKeyFromObject(resource),
    }
    
    result, err := reconciler.Reconcile(ctx, req)
    require.NoError(t, err)
    
    // Verify reconciliation results
    // Test status updates, finalizers, etc.
}
```

#### Testing with Multiple Resources

```go
// Test complex scenarios with multiple resources
func TestMultiResourceScenario(t *testing.T) {
    // Create multiple related resources
    // Test cross-resource interactions
    // Verify cleanup behavior
    // Test error scenarios
}
```

### E2E Testing with envtest

#### Complete E2E Scenarios

```go
// test/e2e/e2e_test.go - All E2E tests use envtest
func TestE2EScenarios(t *testing.T) {
    ctx := context.Background()
    
    // Test complete operator lifecycle
    t.Run("Resource Lifecycle", func(t *testing.T) {
        // Test resource creation, update, deletion
        // Verify finalizer behavior
        // Test status updates
    })
    
    t.Run("Error Scenarios", func(t *testing.T) {
        // Test invalid resource specs
        // Test webhook validations
        // Test reconciliation failures
    })
    
    t.Run("Complex Workflows", func(t *testing.T) {
        // Test multiple resources interaction
        // Test concurrent operations
        // Test scaling scenarios
    })
}
```

#### Performance Testing with envtest

```go
func TestPerformance(t *testing.T) {
    // Test reconciliation performance
    // Test with many resources
    // Measure reconciliation times
    
    start := time.Now()
    // Perform operations
    duration := time.Since(start)
    
    // Assert performance criteria
    assert.Less(t, duration, time.Second*5)
}
```

### Testing Commands Summary

```bash
# RECOMMENDED - All testing with envtest (no cluster needed)
make test                    # Complete test suite with envtest
make generate manifests      # Generate code and manifests
go test -v ./...            # Verbose test output
go test -race ./...         # Tests with race detection
go test -coverprofile=coverage.out ./...  # Tests with coverage

# Test specific packages
go test -v ./api/...        # Test API and webhooks
go test -v ./controllers/... # Test controllers
go test -v ./test/e2e/...   # Test E2E scenarios

# Performance testing
go test -v -timeout 30m ./test/performance/

# PRODUCTION DEPLOYMENT (separate process, not for testing)
# Only when ready to deploy to production environment
# make deploy  # Deploy to production cluster
```

### Continuous Integration Testing

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v3
      with:
        go-version: '1.21'
    
    - name: Download dependencies
      run: go mod download
    
    # All tests use envtest - no cluster required
    - name: Run unit tests
      run: make test
    
    - name: Run tests with race detection
      run: go test -race ./...
    
    - name: Run tests with coverage
      run: |
        go test -v -coverprofile=coverage.out ./...
        go tool cover -html=coverage.out -o coverage.html
    
    - name: Upload coverage reports
      uses: codecov/codecov-action@v3
      with:
        file: ./coverage.out
    
    - name: Run E2E tests
      run: go test -v ./test/e2e/...
    
    # Validate manifests generation
    - name: Verify generated manifests
      run: |
        make generate manifests
        git diff --exit-code # Ensure no changes
```

## Security

### RBAC Best Practices
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: manager-role
rules:
# Principle of least privilege - only necessary permissions
- apiGroups: [""]
  resources: ["configmaps", "secrets"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
```

### Service Account Security
- Use dedicated service accounts
- Avoid cluster-admin permissions
- Implement RBAC with minimal required permissions
- Use Pod Security Standards

### Secure Defaults
```go
// Use secure defaults
spec := &appsv1.DeploymentSpec{
    Template: corev1.PodTemplateSpec{
        Spec: corev1.PodSpec{
            SecurityContext: &corev1.PodSecurityContext{
                RunAsNonRoot:        &[]bool{true}[0],
                RunAsUser:          &[]int64{1000}[0],
                FSGroup:            &[]int64{2000}[0],
                SeccompProfile: &corev1.SeccompProfile{
                    Type: corev1.SeccompProfileTypeRuntimeDefault,
                },
            },
        },
    },
}
```

## Performance

### Efficient Watches and Predicates

```go
// Use predicates to reduce unnecessary reconciliations
func (r *MyResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&myv1.MyResource{}).
        Owns(&appsv1.Deployment{}).
        Watches(
            &corev1.ConfigMap{},
            handler.EnqueueRequestsFromMapFunc(r.findResourcesForConfigMap),
            builder.WithPredicates(
                predicate.ResourceVersionChangedPredicate{}, // Only on actual changes
                predicate.GenerationChangedPredicate{},      // Only on spec changes
            ),
        ).
        WithOptions(controller.Options{
            MaxConcurrentReconciles: 3, // Adjust based on workload
        }).
        Complete(r)
}

// Custom predicate example
type AnnotationChangedPredicate struct {
    AnnotationKey string
}

func (p AnnotationChangedPredicate) Update(e event.UpdateEvent) bool {
    if e.ObjectOld == nil || e.ObjectNew == nil {
        return false
    }
    
    oldAnnotations := e.ObjectOld.GetAnnotations()
    newAnnotations := e.ObjectNew.GetAnnotations()
    
    return oldAnnotations[p.AnnotationKey] != newAnnotations[p.AnnotationKey]
}
```

### Optimized Status Updates

```go
// Use meta.SetStatusCondition for automatic optimization
func (r *MyResourceReconciler) updateStatus(ctx context.Context, resource *myv1.MyResource) error {
    // meta.SetStatusCondition only updates LastTransitionTime if status actually changes
    // This prevents unnecessary API server writes
    
    // Check if status actually needs updating
    if resource.Status.ObservedGeneration == resource.Generation {
        readyCondition := meta.FindStatusCondition(resource.Status.Conditions, ConditionReady)
        if readyCondition != nil && readyCondition.Status == metav1.ConditionTrue {
            return nil // No update needed
        }
    }
    
    return r.Status().Update(ctx, resource)
}
```

### Memory and CPU Optimization

- Use field selectors when possible
- Implement proper predicates to filter events
- Avoid watching too many resources
- Use context cancellation for long-running operations
- Pre-allocate slices when size is known

### Resource Management
```go
// Set resource limits for the operator
resources := corev1.ResourceRequirements{
    Limits: corev1.ResourceList{
        corev1.ResourceCPU:    resource.MustParse("100m"),
        corev1.ResourceMemory: resource.MustParse("128Mi"),
    },
    Requests: corev1.ResourceList{
        corev1.ResourceCPU:    resource.MustParse("50m"),
        corev1.ResourceMemory: resource.MustParse("64Mi"),
    },
}
```

### Concurrent Reconcilers
```go
func (r *MyResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&myv1.MyResource{}).
        WithOptions(controller.Options{
            MaxConcurrentReconciles: 3, // Adjust based on workload
        }).
        Complete(r)
}
```

## Documentation

### Code Documentation
- Document all public APIs
- Include examples in GoDoc comments
- Document controller behavior and assumptions

### User Documentation
- Provide clear installation instructions
- Document all CRD fields and their purpose
- Include usage examples and tutorials
- Document troubleshooting steps

### API Documentation
```go
// MyResource is the Schema for the myresources API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mr
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type MyResource struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   MyResourceSpec   `json:"spec,omitempty"`
    Status MyResourceStatus `json:"status,omitempty"`
}
```

## CI/CD

### Automated Testing
```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v3
      with:
        go-version: '1.20'
    - name: Run tests
      run: make test
    - name: Run integration tests
      run: make test-integration
```

### Security Scanning
- Use tools like `gosec` for Go security scanning
- Scan container images for vulnerabilities
- Implement dependency scanning

### Linting and Formatting
```makefile
.PHONY: fmt vet lint
fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run
```

## Deployment

### Multi-Stage Dockerfile
```dockerfile
# Build stage
FROM golang:1.20 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o manager cmd/main.go

# Runtime stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
```

### Kustomize Configuration
- Use kustomize for environment-specific configurations
- Separate base configurations from overlays
- Use proper resource naming conventions

### Helm Charts (Optional)
- Provide Helm charts for easier deployment
- Use proper templating and values
- Include RBAC and CRD installation

## Monitoring and Observability

### Metrics
```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
    reconcileTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "controller_reconcile_total",
            Help: "Total number of reconciliations performed",
        },
        []string{"controller", "result"},
    )
)

func init() {
    metrics.Registry.MustRegister(reconcileTotal)
}
```

### Logging
```go
import (
    "sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *MyResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx).WithValues("myresource", req.NamespacedName)
    
    log.Info("Starting reconciliation")
    // ... reconciliation logic
    log.Info("Reconciliation completed successfully")
    
    return ctrl.Result{}, nil
}
```

### Health Checks
```go
import (
    "sigs.k8s.io/controller-runtime/pkg/healthz"
)

func main() {
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        HealthProbeBindAddress: ":8081",
    })
    
    if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
        setupLog.Error(err, "unable to set up health check")
        os.Exit(1)
    }
}
```

## Advanced Patterns and Best Practices

### Strategic Merge Patches

For complex resource updates, use strategic merge patches:

```go
import (
    "k8s.io/apimachinery/pkg/util/strategicpatch"
    "k8s.io/apimachinery/pkg/runtime"
)

func (r *MyResourceReconciler) applyStrategicPatch(ctx context.Context, original, updated *unstructured.Unstructured) error {
    // Convert to JSON for patch calculation
    originalJSON, err := original.MarshalJSON()
    if err != nil {
        return err
    }
    
    updatedJSON, err := updated.MarshalJSON()
    if err != nil {
        return err
    }
    
    // Calculate strategic merge patch
    patchBytes, err := strategicpatch.CreateTwoWayMergePatch(
        originalJSON,
        updatedJSON,
        updated.GetObjectKind().GroupVersionKind(),
    )
    if err != nil {
        return err
    }
    
    // Apply patch if not empty
    if len(patchBytes) > 2 { // More than just "{}"
        return r.Patch(ctx, original, client.RawPatch(types.StrategicMergePatchType, patchBytes))
    }
    
    return nil
}
```

### Multi-Resource Reconciliation

```go
func (r *MyResourceReconciler) reconcileResources(ctx context.Context, resource *myv1.MyResource) error {
    // Use errgroup for concurrent resource reconciliation
    g, gctx := errgroup.WithContext(ctx)
    
    // Reconcile dependencies concurrently
    g.Go(func() error {
        return r.reconcileConfigMaps(gctx, resource)
    })
    
    g.Go(func() error {
        return r.reconcileSecrets(gctx, resource)
    })
    
    g.Go(func() error {
        return r.reconcileServices(gctx, resource)
    })
    
    // Wait for all to complete
    if err := g.Wait(); err != nil {
        return err
    }
    
    // Reconcile deployments after dependencies are ready
    return r.reconcileDeployments(ctx, resource)
}
```

### Robust Error Handling

```go
func (r *MyResourceReconciler) reconcileWithRetry(ctx context.Context, resource *myv1.MyResource) (ctrl.Result, error) {
    const maxRetries = 3
    
    for attempt := 0; attempt < maxRetries; attempt++ {
        if err := r.processResource(ctx, resource); err != nil {
            if apierrors.IsConflict(err) {
                // Refresh resource and retry
                if refErr := r.Get(ctx, client.ObjectKeyFromObject(resource), resource); refErr != nil {
                    return ctrl.Result{}, refErr
                }
                continue
            }
            
            if apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) {
                // Temporary error - requeue with exponential backoff
                backoff := time.Duration(attempt+1) * time.Second * 30
                return ctrl.Result{RequeueAfter: backoff}, nil
            }
            
            // Permanent error
            return ctrl.Result{}, err
        }
        
        // Success
        return ctrl.Result{}, nil
    }
    
    return ctrl.Result{RequeueAfter: time.Minute * 5}, fmt.Errorf("max retries exceeded")
}
```

## Event Generation Best Practices

### Event Types and Reasons
Use consistent event types and reasons across your controllers:

```go
// Standard event reasons for common operations
const (
    // Finalizer events
    EventReasonFinalizerAdded      = "FinalizerAdded"
    EventReasonFinalizerRemoved    = "FinalizerRemoved"
    EventReasonFinalizerFailed     = "FinalizerFailed"
    
    // Validation events
    EventReasonValidationSucceeded = "ValidationSucceeded"
    EventReasonValidationFailed    = "ValidationFailed"
    
    // Application events
    EventReasonApplied             = "Applied"
    EventReasonPartiallyApplied    = "PartiallyApplied"
    EventReasonUpToDate            = "UpToDate"
    EventReasonApplicationFailed   = "ApplicationFailed"
)
```

### Event Generation Guidelines
1. **Generate events for user-visible state changes**
2. **Use operation results from CreateOrPatch to determine event necessity**
3. **Provide meaningful, actionable messages**
4. **Use Normal events for successful operations, Warning for failures**
5. **Don't generate events for every reconciliation - only when state changes**

### Example: Comprehensive Event Coverage

```go
func (r *MyResourceReconciler) applyConfiguration(ctx context.Context, resource *myv1.MyResource, target client.Object) error {
    op, err := controllerutil.CreateOrPatch(ctx, r.Client, target, func() error {
        // Apply configuration within mutation function
        return r.applyResourceTemplate(resource, target)
    })
    
    if err != nil {
        r.Recorder.Event(resource, corev1.EventTypeWarning, EventReasonApplicationFailed,
            fmt.Sprintf("Failed to apply configuration to %s/%s: %v", 
                target.GetNamespace(), target.GetName(), err))
        return err
    }
    
    // Generate events based on operation result
    switch op {
    case controllerutil.OperationResultCreated:
        r.Recorder.Event(resource, corev1.EventTypeNormal, EventReasonApplied,
            fmt.Sprintf("Configuration applied to new resource %s/%s", 
                target.GetNamespace(), target.GetName()))
    case controllerutil.OperationResultUpdated:
        r.Recorder.Event(resource, corev1.EventTypeNormal, EventReasonApplied,
            fmt.Sprintf("Configuration updated on %s/%s", 
                target.GetNamespace(), target.GetName()))
    case controllerutil.OperationResultNone:
        r.Recorder.Event(resource, corev1.EventTypeNormal, EventReasonUpToDate,
            fmt.Sprintf("Configuration already up to date on %s/%s", 
                target.GetNamespace(), target.GetName()))
    }
    
    return nil
}
```

## Common Pitfalls to Avoid

1. **Don't set LastTransitionTime manually** - Let `meta.SetStatusCondition` handle it for optimal performance
2. **Don't use phases as primary status** - Use conditions for modern APIs, phases only for backward compatibility
3. **Don't ignore errors** - Always handle errors appropriately with proper retry logic
4. **Don't forget finalizers** - Implement proper cleanup logic for resource dependencies
5. **Don't make assumptions about resource existence** - Always check if resources exist before operations
6. **Don't use infinite requeue** - Implement exponential backoff strategies
7. **Don't skip status updates** - Keep users informed about resource state with meaningful conditions
8. **Don't hardcode values** - Use configuration and environment variables
9. **Don't ignore RBAC** - Implement minimal required permissions following principle of least privilege
10. **Don't skip testing** - Use envtest for all development and testing, never connect to real clusters
11. **Don't update status unnecessarily** - Check if updates are needed before making API calls
12. **Don't ignore resource conflicts** - Handle update conflicts with proper retry mechanisms
13. **Don't use blocking operations** - Use context cancellation and timeouts for long-running operations
14. **Don't forget observability** - Implement proper logging, metrics, and health checks
15. **Don't modify objects before CreateOrPatch** - Always make modifications within the mutation function
16. **Don't generate events for every reconciliation** - Use operation results to determine when events are needed
17. **Don't forget to fetch latest resource version before status updates** - Prevents resource version conflicts
18. **Don't use empty mutation functions in CreateOrPatch** - Ensure the mutation function actually modifies the object

## Code Quality and Linting Best Practices

### GoDoc Comment Standards
Ensure all GoDoc comments start with the exact name of the field, function, or type:

```go
// ✅ CORRECT
// ProfileSpec defines the configuration template to apply to target resources
type ProfileSpec struct {
    // Name identifies the profile within its scope
    Name string `json:"name"`
}

// ❌ INCORRECT
// Defines the configuration template to apply to target resources
type ProfileSpec struct {
    // Identifier for the profile within its scope
    Name string `json:"name"`
}
```

### Controller Struct Best Practices
Add appropriate JSON tags for controller structs to satisfy linters:

```go
type ProfileReconciler struct {
    client.Client    `json:",inline"`
    Scheme   *runtime.Scheme      `json:"-"`
    Recorder record.EventRecorder `json:"-"`
}
```

### Function Signature Optimization
Avoid functions that always return nil errors - simplify the signature:

```go
// ✅ PREFERRED - Simple signature
func (r *MyReconciler) hasChanges(original, updated *unstructured.Unstructured) bool {
    return !r.areEqual(original, updated)
}

// ❌ AVOID - Unnecessary error return
func (r *MyReconciler) hasChanges(original, updated *unstructured.Unstructured) (bool, error) {
    return !r.areEqual(original, updated), nil
}
```

### Embedded Field Access Optimization
Prefer direct field access over embedded field selectors for better readability:

```go
// ✅ PREFERRED
if !resource.DeletionTimestamp.IsZero() {
    return r.reconcileDelete(ctx, resource)
}

// ❌ VERBOSE (still correct but less preferred)
if !resource.ObjectMeta.DeletionTimestamp.IsZero() {
    return r.reconcileDelete(ctx, resource)
}
```

### Text Encoding and File Formatting
- Always use `go fmt` to format files properly
- Be careful with escaped characters in string literals
- Use proper tab characters in code, not escaped sequences
- Run `make test` regularly to catch formatting issues early

### Kubebuilder Marker Best Practices
Ensure proper kubebuilder marker syntax and avoid duplicates:

```go
// ✅ CORRECT - Single validation marker
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=100
Name string `json:"name"`

// ❌ INCORRECT - Duplicate markers
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MinLength=1
Name string `json:"name"`
```

## Debugging and Troubleshooting Strategies

### Systematic Problem Resolution Approach

#### 1. Compilation Errors
```bash
# Always start with basic compilation
go build ./...

# Check for formatting issues
go fmt ./...

# Verify imports and dependencies
go mod tidy
```

#### 2. Linter Warnings and Errors
```bash
# Generate manifests and code
make generate manifests

# Run comprehensive linting
make lint

# Check for specific kubebuilder issues
controller-gen --help
```

#### 3. Test Failures
```bash
# Run tests with verbose output
go test -v ./...

# Test specific packages
go test -v ./internal/controller/...

# Run with race detection
go test -race ./...
```

#### 4. Common Error Categories and Solutions

**GoDoc Comment Issues:**
- Problem: Comments don't start with field/function name
- Solution: Ensure first word matches exactly the identifier name

**Kubebuilder Marker Issues:**
- Problem: Duplicate or malformed validation markers
- Solution: Review all `+kubebuilder:` comments for syntax and duplicates

**JSON Tag Issues:**
- Problem: Missing JSON tags on struct fields
- Solution: Add appropriate `json:"fieldname"` or `json:"-"` tags

**Embedded Field Access:**
- Problem: Unnecessary embedded field selectors
- Solution: Use direct field access when the field is not ambiguous

**Function Signature Issues:**
- Problem: Functions returning errors that are always nil
- Solution: Simplify function signature to remove unused error returns

### Step-by-Step Fix Process

1. **Identify Error Categories**: Group similar errors together
2. **Fix Systematically**: Address one category at a time
3. **Test Incrementally**: Run tests after each major fix
4. **Verify Building**: Ensure `go build ./...` succeeds
5. **Run Full Test Suite**: Execute `make test` for final validation

### Prevention Strategies

- Use consistent code formatting with `go fmt`
- Follow Go naming conventions strictly
- Write comprehensive tests for all functionality
- Use kubebuilder markers correctly from the start
- Regular linting and code review
- Maintain clean git history with meaningful commits

## Useful Resources

- [Kubebuilder Documentation](https://book.kubebuilder.io/)
- [Controller Runtime Documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Operator SDK Best Practices](https://sdk.operatorframework.io/docs/best-practices/)
- [CNCF Operator White Paper](https://github.com/cncf/tag-app-delivery/blob/main/operator-whitepaper/Operator-WhitePaper_v1-0.md)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go.html)

---

Following these best practices will help you build robust, maintainable, and production-ready Kubernetes operators using Kubebuilder. The debugging strategies will help you systematically resolve issues and maintain code quality throughout development.
