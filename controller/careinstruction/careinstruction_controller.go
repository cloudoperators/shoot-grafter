package careinstruction

import (
	"context"
	"errors"
	"reflect"

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

//+kubebuilder:rbac:groups=shoot-grafter.cloudoperators,resources=careinstructions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=greenhouse.sap,resources=clusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

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
		r.Error(err, "failed to reconcile shoots and clusters for CareInstruction")
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
	if r.gardens == nil {
		r.gardens = make(map[string]*garden)
	}
	if _, exists := r.gardens[careInstruction.Name]; !exists {
		r.gardens[careInstruction.Name] = &garden{
			mgr:                 nil,
			gardenConfig:        nil,
			gardenClient:        nil,
			careInstructionSpec: &careInstruction.Spec,
			cancelFunc:          nil,
		}
	}

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

	// TODO: Need to recreate the manager if the careinstruction changes (at least the labels)

	// Now we check the following to see if we need to recreate and restart the manager:
	// 1. If we have no manager for the garden cluster, we need to create one
	mgrExists := r.gardens[careInstruction.Name].mgr != nil
	// 2. If the manager could not be created, we need to recreate the client and manager
	shootControllerStarted := careInstruction.Status.GetConditionByType(v1alpha1.ShootControllerStartedCondition).IsTrue()
	// 3. If the created config is different to the existing one, we need to recreate the client and manager
	gardenConfigChanged := !reflect.DeepEqual(r.gardens[careInstruction.Name].gardenConfig, &gardenClientConfig)
	// 4. If the CareInstruction.Spec has changed, we need to recreate the client and manager
	careInstructionSpecChanged := !reflect.DeepEqual(*r.gardens[careInstruction.Name].careInstructionSpec, careInstruction.Spec)
	// 5. This is a safeguard: if the stop channel is nil or closed, we need to recreate the manager
	channelExists := r.gardens[careInstruction.Name].stopChan != nil
	channelOpen := true
	if channelExists {
		// use select with default for non-blocking check to see if the channel is closed
		// https://gobyexample.com/non-blocking-channel-operations
		select {
		case _, ok := <-r.gardens[careInstruction.Name].stopChan:
			channelOpen = ok
		default:
			channelOpen = true
		}
	}
	// TODO: kill existing manager if careInstruction changes
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
	// Stop the existing manager if it exists
	if mgrExists {
		r.gardens[careInstruction.Name].cancelFunc()
		r.Info("Stopped existing garden manager for shootController", "shootControllerName", shoot.GenerateName(careInstruction.Name), "careInstruction", careInstruction.Name)
	}

	r.gardens[careInstruction.Name].gardenConfig = &gardenClientConfig

	// Create a new client for the garden cluster
	gardenClient, err := client.New(&gardenClientConfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	r.gardens[careInstruction.Name].gardenClient = &gardenClient
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
	sc := &shoot.ShootController{
		GreenhouseClient: r.Client,
		GardenClient:     gardenClient,
		Logger:           r.WithValues("careInstruction", careInstruction.Name),
		Name:             shoot.GenerateName(careInstruction.Name),
		CareInstruction:  careInstruction.DeepCopy(),
	}
	if err := sc.SetupWithManager(shootControllerMgr); err != nil {
		return err
	}
	r.Info("Successfully created shoot controller manager for garden cluster with shoot controller", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)

	// Create a new context for the garden manager from the main context with cancel function
	gardenMgrContext, cancel := context.WithCancel(r.GardenMgrContextFunc())
	r.gardens[careInstruction.Name].mgr = shootControllerMgr
	r.gardens[careInstruction.Name].cancelFunc = cancel
	r.gardens[careInstruction.Name].stopChan = make(chan bool)
	// Start the garden manager in a goroutine
	go func() {
		defer close(r.gardens[careInstruction.Name].stopChan)
		log.FromContext(gardenMgrContext).Info("Starting garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
		if err := shootControllerMgr.Start(gardenMgrContext); err != nil {
			log.FromContext(gardenMgrContext).Error(err, "Failed to start garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
			return
		}
		log.FromContext(gardenMgrContext).Info("Shut down garden manager", "shootControllerName", sc.Name, "careInstruction", careInstruction.Name)
	}()

	// store latest careInstruction.Spec if manager started successfully
	r.gardens[careInstruction.Name].careInstructionSpec = &careInstruction.Spec

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
	careInstruction.Status.TotalShoots = 0
	careInstruction.Status.CreatedClusters = 0
	careInstruction.Status.FailedClusters = 0
	// List all shoots targeted by the given CareInstruction
	shoots, err := careInstruction.ListShoots(ctx, *r.gardens[careInstruction.Name].gardenClient)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if apierrors.IsNotFound(err) {
		careInstruction.Status.TotalShoots = 0
	} else {
		careInstruction.Status.TotalShoots = len(shoots.Items)
	}
	// List all clusters created by this CareInstruction
	clusters, err := careInstruction.ListClusters(ctx, r.Client, careInstruction.Namespace)
	if client.IgnoreNotFound(err) != nil {
		return err
	}
	if apierrors.IsNotFound(err) {
		careInstruction.Status.CreatedClusters = 0
	} else {
		careInstruction.Status.CreatedClusters = len(clusters.Items)
	}

	// TODO: Check if we really error out here
	// Check if created TotalCreatedClusters match TotalShoots
	if careInstruction.Status.TotalShoots != careInstruction.Status.CreatedClusters {
		err := errors.New("total shoots and created clusters do not match")
		return err
	}

	// TODO: Check if we really error out here
	// TODO dont error out on first not ready cluster, need to check all clusters
	// Check if all Clusters are ready
	for _, cluster := range clusters.Items {
		if !cluster.Status.IsReadyTrue() {
			careInstruction.Status.FailedClusters++
		}
		if careInstruction.Status.FailedClusters > 0 {
			err := errors.New("cluster is not ready")

			return err
		}
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
	// Cancel the garden manager context for this garden cluster
	if garden, exists := r.gardens[careInstruction.Name]; exists {
		if garden.cancelFunc != nil {
			garden.cancelFunc()
			delete(r.gardens, careInstruction.Name)
			r.Info("Cancelled garden manager context for shootController and deleted manager", "shootControllerName", shoot.GenerateName(careInstruction.Name), "careInstruction", careInstruction.Name)
		}
	} else {
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

// enqueCareInstructionForGardenCluster - enqueues the CareInstruction for the given Garden Cluster.
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
