// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company and Greenhouse contributors
// SPDX-License-Identifier: Apache-2.0

package careinstruction

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"shoot-grafter/api/v1alpha1"
	"shoot-grafter/controller/shoot"
	"shoot-grafter/internal/clientutil"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	greenhouseapis "github.com/cloudoperators/greenhouse/api"
	greenhousemetav1alpha1 "github.com/cloudoperators/greenhouse/api/meta/v1alpha1"
	greenhousev1alpha1 "github.com/cloudoperators/greenhouse/api/v1alpha1"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CareInstructionReconciler reconciles a CareInstruction object and dynamically starts controllers watching shoots for the seed cluster provided in the CareInstruction.
type CareInstructionReconciler struct {
	client.Client
	logr.Logger
	ctrl.Manager
	// GardenMgr needs long running context not from the Reconcile function.
	// Using factory function, expecting it to be set from the instantiating context.
	GardenMgrContextFunc func() context.Context
	gardens              map[string]*garden // Map of garden cluster names to their respective garden objects
	gardensMu            sync.RWMutex       // Mutex to protect concurrent access to gardens map
}

type garden struct {
	mgr                 ctrl.Manager                  // The manager for the garden cluster
	gardenConfig        *rest.Config                  // The REST config for the garden cluster
	gardenClient        *client.Client                // The client for the garden cluster
	careInstructionSpec *v1alpha1.CareInstructionSpec // The CareInstruction object for the garden cluster
	cancelFunc          context.CancelFunc            // Cancel function to stop the manager
	stopChan            chan bool                     // Channel to know if the manager is stopped
}

type careInstructionContextKey struct{}

//+kubebuilder:rbac:groups=shoot-grafter.cloudoperators.dev,resources=careinstructions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=shoot-grafter.cloudoperators.dev,resources=careinstructions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=greenhouse.sap,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;delete

func (r *CareInstructionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Setup the controller with the manager
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.CareInstruction{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.enqueueCareInstructionForGardenCluster), builder.WithPredicates(clientutil.PredicateFilterBySecretTypes(greenhouseapis.SecretTypeKubeConfig, greenhouseapis.SecretTypeOIDCConfig))).
		Watches(&greenhousev1alpha1.Cluster{}, handler.EnqueueRequestsFromMapFunc(r.enqueueCareInstructionForCreatedClusters), builder.WithPredicates(clientutil.PredicateHasLabel(v1alpha1.CareInstructionLabel))).
		Complete(r)
}

func (r *CareInstructionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Logger = ctrl.LoggerFrom(ctx)
	r.Info("Reconciling CareInstruction", "name", req.Name, "namespace", req.Namespace)

	var careInstruction v1alpha1.CareInstruction
	if err := r.Get(ctx, req.NamespacedName, &careInstruction); err != nil {
		r.Error(err, "unable to fetch CareInstruction")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// initialize the context with the original CareInstruction object
	ctx = context.WithValue(ctx, careInstructionContextKey{}, careInstruction.DeepCopyObject())
	shouldBeDeleted := (careInstruction.GetDeletionTimestamp() != nil)
	hasFinalizer := controllerutil.ContainsFinalizer(&careInstruction, v1alpha1.CommonCleanupFinalizer)

	// check whether finalizer is set
	if !shouldBeDeleted && !hasFinalizer {
		return ctrl.Result{}, r.ensureFinalizer(ctx, &careInstruction, v1alpha1.CommonCleanupFinalizer)
	}

	// Initialize conditions to unknown if not set
	initializeConditionsToUnknown(&careInstruction)
	defer func(careInstruction *v1alpha1.CareInstruction) {
		if statusErr := r.reconcileStatus(ctx, careInstruction); statusErr != nil {
			r.Error(statusErr, "failed to reconcile status")
		}
	}(&careInstruction)

	// Deletion logic
	if shouldBeDeleted {
		// reconcile again for finalizer
		if !hasFinalizer {
			return ctrl.Result{}, nil
		}
		err := r.cleanupCareInstruction(ctx, &careInstruction)
		return ctrl.Result{}, err
	}

	if err := r.reconcileManager(ctx, careInstruction); err != nil {
		r.Error(err, "failed to reconcile manager for garden cluster")
		careInstruction.Status.SetConditions(
			greenhousemetav1alpha1.FalseCondition(
				v1alpha1.ShootControllerStartedCondition,
				"ShootControllerError",
				err.Error(),
			),
		)
		return ctrl.Result{}, err
	}

	// reconcile Shoots and Clusters created by this CareInstruction
	if err := r.reconcileShootsNClusters(ctx, &careInstruction); err != nil {
		r.Info("failed to reconcile shoots and clusters for CareInstruction, will retry", "error", err.Error())
		careInstruction.Status.SetConditions(
			greenhousemetav1alpha1.FalseCondition(
				v1alpha1.ShootsReconciledCondition,
				"ShootsReconciledError",
				err.Error(),
			),
		)
	}

	return ctrl.Result{}, nil
}

// ensureFinalizer - ensures a finalizer is present on the object. Returns an error on failure.
func (r *CareInstructionReconciler) ensureFinalizer(ctx context.Context, o client.Object, finalizer string) error {
	if controllerutil.AddFinalizer(o, finalizer) {
		return r.Update(ctx, o)
	}
	return nil
}

// removeFinalizer - removes a finalizer from an object. Returns an error on failure.
func (r *CareInstructionReconciler) removeFinalizer(ctx context.Context, o client.Object, finalizer string) error {
	if controllerutil.RemoveFinalizer(o, finalizer) {
		return r.Update(ctx, o)
	}
	return nil
}

// reconcileManager - reconciles the shoot controller manager for the given CareInstruction.
func (r *CareInstructionReconciler) reconcileManager(ctx context.Context, careInstruction v1alpha1.CareInstruction) error {
	r.Info("Reconciling shoot controller manager for garden cluster", "name", careInstruction.Spec.GardenClusterName)

	// Use namespace-qualified key to prevent collisions between CareInstructions with the same name in different namespaces
	gardenKey := careInstruction.Namespace + "/" + careInstruction.Name

	// Initialize gardens map if needed (with write lock)
	r.gardensMu.Lock()
	if r.gardens == nil {
		r.gardens = make(map[string]*garden)
	}
	if _, exists := r.gardens[gardenKey]; !exists {
		r.gardens[gardenKey] = &garden{
			mgr:                 nil,
			gardenConfig:        nil,
			gardenClient:        nil,
			careInstructionSpec: &careInstruction.Spec,
			cancelFunc:          nil,
		}
	}
	r.gardensMu.Unlock()

	gardenClientConfig, scheme, err := r.GetGardenClusterAccess(ctx, &careInstruction)
	// Get Access to the Garden Cluster
	if err != nil {
		careInstruction.Status.SetConditions(
			greenhousemetav1alpha1.FalseCondition(
				v1alpha1.GardenClusterAccessReady,
				"GardenClusterAccessError",
				err.Error(),
			),
		)
		return err
	}
	r.Info("Successfully got client config for garden cluster", "gardenCluster", careInstruction.Spec.GardenClusterName)
	careInstruction.Status.SetConditions(
		greenhousemetav1alpha1.TrueCondition(
			v1alpha1.GardenClusterAccessReady,
			"GardenClusterAccessReady",
			"",
		),
	)

	// Now we check the following to see if we need to recreate and restart the manager (with read lock):
	r.gardensMu.RLock()
	garden := r.gardens[gardenKey]
	// 1. If we have no manager for the garden cluster, we need to create one
	mgrExists := garden.mgr != nil
	// 2. If the manager could not be created, we need to recreate the client and manager
	shootControllerStarted := careInstruction.Status.GetConditionByType(v1alpha1.ShootControllerStartedCondition).IsTrue()
	// 3. If the created config is different to the existing one, we need to recreate the client and manager
	gardenConfigChanged := !reflect.DeepEqual(garden.gardenConfig, &gardenClientConfig)
	// 4. If the CareInstruction.Spec has changed, we need to recreate the client and manager
	careInstructionSpecChanged := !reflect.DeepEqual(*garden.careInstructionSpec, careInstruction.Spec)
	// 5. This is a safeguard: if the stop channel is nil or closed, we need to recreate the manager
	channelExists := garden.stopChan != nil
	channelOpen := true
	if channelExists {
		// use select with default for non-blocking check to see if the channel is closed
		// https://gobyexample.com/non-blocking-channel-operations
		select {
		case _, ok := <-garden.stopChan:
			channelOpen = ok
		default:
			channelOpen = true
		}
	}
	r.gardensMu.RUnlock()

	if mgrExists && shootControllerStarted && !gardenConfigChanged && !careInstructionSpecChanged && channelExists && channelOpen {
		r.Info("Manager is running, garden cluster config & careInstruction.Spec is unchanged, skipping client and manager recreation", "careInstruction", careInstruction.Name)
		return nil
	}
	var reason string
	switch {
	case !mgrExists:
		reason = "no manager exists"
	case gardenConfigChanged:
		reason = "garden cluster config has changed"
	case careInstructionSpecChanged:
		reason = "careInstruction.Spec has changed"
	case !shootControllerStarted:
		reason = "shoot controller not started"
	case !channelExists:
		reason = "stop channel is missing"
	case !channelOpen:
		reason = "manager stop channel is closed"
	default:
		reason = "unknown reason"
	}
	r.Info("Recreating client and manager for garden cluster because "+reason, "careInstruction", careInstruction.Name)

	// Stop the existing manager if it exists (with read lock for cancelFunc access)
	if mgrExists {
		r.gardensMu.RLock()
		cancelFunc := r.gardens[gardenKey].cancelFunc
		r.gardensMu.RUnlock()

		if cancelFunc != nil {
			cancelFunc()
		}
		r.Info("Stopped existing garden manager for shootController", "shootControllerName", shoot.GenerateName(careInstruction.Name), "careInstruction", careInstruction.Name)
	}

	// Create a new client for the garden cluster
	gardenClient, err := client.New(&gardenClientConfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	r.Info("Successfully created client for garden cluster", "name", careInstruction.Spec.GardenClusterName)

	skipNameValidation := true
	shootControllerMgr, err := ctrl.NewManager(&gardenClientConfig, ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			// Only watch the namespace specified in the CareInstruction
			DefaultNamespaces: map[string]cache.Config{
				careInstruction.Spec.GardenNamespace: {},
			},
		},
		Metrics: server.Options{
			BindAddress: "0", // Disable metrics for the shoot controller manager
		},
		BaseContext: r.GardenMgrContextFunc, // Use the context factory function to get a long-running context
		Controller: config.Controller{
			SkipNameValidation: &skipNameValidation, // Skip name validation for the controller
		},
	})

	if err != nil {
		return err
	}

	// Register the ShootController with the garden manager
	// Note: EventRecorder is obtained from the Greenhouse manager to emit events on the Greenhouse cluster
	sc := &shoot.ShootController{
		GreenhouseClient: r.Client,
		GardenClient:     gardenClient,
		Logger:           r.WithValues("careInstruction", careInstruction.Name),
		Name:             shoot.GenerateName(careInstruction.Name),
		CareInstruction:  careInstruction.DeepCopy(),
		EventRecorder:    r.GetEventRecorderFor(shoot.GenerateName(careInstruction.Name)),
	}
	if err := sc.SetupWithManager(shootControllerMgr); err != nil {
		return err
	}
	r.Info("Successfully created shoot controller manager for garden cluster with shoot controller", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)

	// Create a new context for the garden manager from the main context with cancel function
	gardenMgrContext, cancel := context.WithCancel(r.GardenMgrContextFunc())

	// Update garden state (with write lock)
	r.gardensMu.Lock()
	r.gardens[gardenKey].gardenConfig = &gardenClientConfig
	r.gardens[gardenKey].gardenClient = &gardenClient
	r.gardens[gardenKey].mgr = shootControllerMgr
	r.gardens[gardenKey].cancelFunc = cancel
	r.gardens[gardenKey].stopChan = make(chan bool)
	r.gardens[gardenKey].careInstructionSpec = &careInstruction.Spec
	stopChan := r.gardens[gardenKey].stopChan
	r.gardensMu.Unlock()

	// Start the garden manager in a goroutine
	go func() {
		defer close(stopChan)
		log.FromContext(gardenMgrContext).Info("Starting garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
		if err := shootControllerMgr.Start(gardenMgrContext); err != nil {
			log.FromContext(gardenMgrContext).Error(err, "Failed to start garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
			return
		}
		log.FromContext(gardenMgrContext).Info("Shut down garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
	}()

	careInstruction.Status.SetConditions(
		greenhousemetav1alpha1.TrueCondition(
			v1alpha1.ShootControllerStartedCondition,
			"Started",
			"",
		),
	)

	return nil
}

// reconcileShootsNClusters - reconciles the clusters created and owned by this CareInstruction.
func (r *CareInstructionReconciler) reconcileShootsNClusters(ctx context.Context, careInstruction *v1alpha1.CareInstruction) error {
	r.Info("Reconciling shoots and clusters for CareInstruction", "name", careInstruction.Name, "namespace", careInstruction.Namespace)
	careInstruction.Status.TotalTargetShoots = 0
	careInstruction.Status.CreatedClusters = 0
	careInstruction.Status.FailedClusters = 0
	careInstruction.Status.Shoots = []v1alpha1.ShootStatus{}

	// Get garden client (with read lock)
	gardenKey := careInstruction.Namespace + "/" + careInstruction.Name
	r.gardensMu.RLock()
	gardenClient := r.gardens[gardenKey].gardenClient
	r.gardensMu.RUnlock()

	// List all shoots targeted by this CareInstruction
	shoots, err := careInstruction.ListShoots(ctx, *gardenClient)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if apierrors.IsNotFound(err) {
		careInstruction.Status.TotalTargetShoots = 0
	} else {
		careInstruction.Status.TotalTargetShoots = len(shoots.Items)
	}

	var includedShoots []gardenerv1beta1.Shoot
	for _, shoot := range shoots.Items {
		matches, err := careInstruction.MatchesCELFilter(&shoot)
		switch {
		case err != nil:
			r.Info("CEL evaluation failed", "shoot", shoot.Name, "error", err.Error())
			careInstruction.Status.Shoots = append(careInstruction.Status.Shoots, v1alpha1.ShootStatus{
				Name:    shoot.Name,
				Status:  v1alpha1.ShootStatusExcluded,
				Message: "CEL evaluation failed: " + err.Error(),
			})
		case !matches:
			r.Info("Shoot filtered out by CEL expression", "shoot", shoot.Name)
			careInstruction.Status.Shoots = append(careInstruction.Status.Shoots, v1alpha1.ShootStatus{
				Name:    shoot.Name,
				Status:  v1alpha1.ShootStatusExcluded,
				Message: "filtered out by CEL expression",
			})
		default:
			includedShoots = append(includedShoots, shoot)
		}
	}

	// List all clusters created by this CareInstruction
	clusters, err := careInstruction.ListClusters(ctx, r.Client)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if apierrors.IsNotFound(err) {
		careInstruction.Status.CreatedClusters = 0
	} else {
		careInstruction.Status.CreatedClusters = len(clusters.Items)
	}

	// Build a map of existing cluster names owned by this CareInstruction
	existingClusterNames := make(map[string]bool)
	for _, cluster := range clusters.Items {
		existingClusterNames[cluster.Name] = true
	}

	effectiveShootCount := len(includedShoots)
	if effectiveShootCount != careInstruction.Status.CreatedClusters {
		r.Info("Shoot count does not match cluster count, checking for ownership conflicts",
			"effectiveShoots", effectiveShootCount,
			"createdClusters", careInstruction.Status.CreatedClusters)

		// Only check ownership conflicts for shoots that pass all filters.
		// Filtered-out shoots should not have new clusters created.
		for _, shoot := range includedShoots {
			if existingClusterNames[shoot.Name] {
				continue
			}

			// Check if a cluster exists but is owned by a different CareInstruction
			var existingCluster greenhousev1alpha1.Cluster
			err := r.Get(ctx, client.ObjectKey{Name: shoot.Name, Namespace: careInstruction.Namespace}, &existingCluster)
			if err == nil {
				// Cluster exists - check ownership
				if ownerLabel, hasLabel := existingCluster.Labels[v1alpha1.CareInstructionLabel]; hasLabel && ownerLabel != careInstruction.Name {
					r.Info("Shoot cluster is managed by a different CareInstruction",
						"shoot", shoot.Name,
						"managedBy", ownerLabel,
						"attemptedBy", careInstruction.Name)

					shootStatus := v1alpha1.ShootStatus{
						Name:    shoot.Name,
						Message: "Cluster managed by different CareInstruction: " + ownerLabel,
					}

					if existingCluster.Status.IsReadyTrue() {
						shootStatus.Status = v1alpha1.ShootStatusOnboarded
					} else {
						shootStatus.Status = v1alpha1.ShootStatusFailed
						careInstruction.Status.FailedClusters++
					}

					careInstruction.Status.Shoots = append(careInstruction.Status.Shoots, shootStatus)
				}
			} else if !apierrors.IsNotFound(err) {
				// Some other error occurred
				return err
			}
		}

		err := errors.New("shoot count and cluster count do not match")
		return err
	}

	// Populate detailed shoot status list from clusters
	for _, cluster := range clusters.Items {
		shootStatus := v1alpha1.ShootStatus{
			Name: cluster.Name,
		}

		if cluster.Status.IsReadyTrue() {
			shootStatus.Status = v1alpha1.ShootStatusOnboarded
		} else {
			shootStatus.Status = v1alpha1.ShootStatusFailed
			// Get the message from the Ready condition
			readyCondition := cluster.Status.GetConditionByType(greenhousemetav1alpha1.ReadyCondition)
			if readyCondition != nil && readyCondition.Message != "" {
				shootStatus.Message = readyCondition.Message
			}
			careInstruction.Status.FailedClusters++
		}

		careInstruction.Status.Shoots = append(careInstruction.Status.Shoots, shootStatus)
	}

	if careInstruction.Status.FailedClusters > 0 {
		err := errors.New("cluster is not ready")
		return err
	}

	r.Info("All shoots and clusters are reconciled for CareInstruction", "name", careInstruction.Name, "namespace", careInstruction.Namespace)
	careInstruction.Status.SetConditions(
		greenhousemetav1alpha1.TrueCondition(
			v1alpha1.ShootsReconciledCondition,
			"Reconciled",
			"All shoots and clusters are reconciled",
		),
	)

	return nil
}

// cleanupCareInstruction - deletes the CareInstruction and cleans up any resources associated with it.
func (r *CareInstructionReconciler) cleanupCareInstruction(ctx context.Context, careInstruction *v1alpha1.CareInstruction) error {
	r.Info("Cleaning up CareInstruction", "name", careInstruction.Name, "namespace", careInstruction.Namespace)
	careInstruction.Status.SetConditions(
		greenhousemetav1alpha1.FalseCondition(
			v1alpha1.DeleteCondition,
			"PendingDeletion",
			"CareInstruction is being deleted",
		),
	)

	// Cancel the garden manager context for this garden cluster (with write lock)
	gardenKey := careInstruction.Namespace + "/" + careInstruction.Name
	r.gardensMu.Lock()
	garden, exists := r.gardens[gardenKey]
	if exists && garden.cancelFunc != nil {
		garden.cancelFunc()
		delete(r.gardens, gardenKey)
		r.gardensMu.Unlock()
		r.Info("Cancelled garden manager context for shootController and deleted manager", "shootControllerName", shoot.GenerateName(careInstruction.Name), "careInstruction", careInstruction.Name)
	} else {
		r.gardensMu.Unlock()
		r.Info("Garden manager context not found for careInstruction: " + careInstruction.Name)
	}
	// Remove the finalizer
	if err := r.removeFinalizer(ctx, careInstruction, v1alpha1.CommonCleanupFinalizer); err != nil {
		r.Error(err, "Unable to remove finalizer from CareInstruction", "name", careInstruction.Name)
		careInstruction.Status.SetConditions(
			greenhousemetav1alpha1.FalseCondition(
				v1alpha1.DeleteCondition,
				"FinalizerError",
				err.Error(),
			),
		)
		return err
	}
	r.Info("Removed finalizer from CareInstruction", "name", careInstruction.Name)
	return nil
}

// enqueueCareInstructionForGardenCluster - enqueues the CareInstruction for the given Garden Cluster.
func (r *CareInstructionReconciler) enqueueCareInstructionForGardenCluster(_ context.Context, obj client.Object) []ctrl.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		r.Error(errors.New("object is not a Secret"), "Expected a Secret object")
		return nil
	}

	r.Info("Enqueuing CareInstruction for Garden Cluster", "name", secret.Name)

	// Find all CareInstructions that reference this Garden Cluster
	var careInstructions v1alpha1.CareInstructionList
	if err := r.List(context.Background(), &careInstructions); err != nil {
		r.Error(err, "Failed to list CareInstructions")
		return nil
	}

	var requests []ctrl.Request
	for _, careInstruction := range careInstructions.Items {
		if careInstruction.Spec.GardenClusterName == secret.Name {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      careInstruction.Name,
					Namespace: careInstruction.Namespace,
				},
			})
			r.Info("Enqueued CareInstruction for Garden Cluster", "careInstruction", careInstruction.Name)
		}
	}

	if len(requests) == 0 {
		r.Info("No CareInstructions found for Garden Cluster", "name", secret.Name)
	}

	return requests
}

func (r *CareInstructionReconciler) enqueueCareInstructionForCreatedClusters(_ context.Context, obj client.Object) []ctrl.Request {
	cluster, ok := obj.(*greenhousev1alpha1.Cluster)
	if !ok {
		return nil
	}

	// Check if the cluster has the CareInstruction label
	careInstructionName, exists := cluster.Labels[v1alpha1.CareInstructionLabel]
	if !exists {
		return nil
	}

	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{
				Name:      careInstructionName,
				Namespace: cluster.Namespace,
			},
		},
	}
}
