/*
Copyright 2022.

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

package controller

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sync/errgroup"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/descriptor/provider"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/pkg/adapter"
	"github.com/kyma-project/lifecycle-manager/pkg/log"
	"github.com/kyma-project/lifecycle-manager/pkg/module/common"
	"github.com/kyma-project/lifecycle-manager/pkg/module/parse"
	"github.com/kyma-project/lifecycle-manager/pkg/module/sync"
	"github.com/kyma-project/lifecycle-manager/pkg/queue"
	"github.com/kyma-project/lifecycle-manager/pkg/remote"
	"github.com/kyma-project/lifecycle-manager/pkg/status"
	"github.com/kyma-project/lifecycle-manager/pkg/templatelookup"
	"github.com/kyma-project/lifecycle-manager/pkg/util"
)

type (
	EventReasonError string
	EventReasonInfo  string
)

var ErrManifestsStillExist = errors.New("manifests still exist")

const (
	moduleReconciliationError EventReasonError = "ModuleReconciliationError"
	metricsError              EventReasonError = "MetricsError"
	deletionError             EventReasonError = "DeletionError"
	updateStatus              EventReasonInfo  = "StatusUpdate"
)

type KymaReconciler struct {
	client.Client
	record.EventRecorder
	queue.RequeueIntervals
	DescriptorProvider  *provider.CachedDescriptorProvider
	KcpRestConfig       *rest.Config
	RemoteSyncNamespace string
	IsManagedKyma       bool
	Metrics             *metrics.KymaMetrics
}

// +kubebuilder:rbac:groups=operator.kyma-project.io,resources=kymas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.kyma-project.io,resources=kymas/status,verbs=get;update;patch;watch
// +kubebuilder:rbac:groups=operator.kyma-project.io,resources=kymas/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.kyma-project.io,resources=moduletemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.kyma-project.io,resources=moduletemplates/finalizers,verbs=update
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=issuers,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;create;update;delete;patch;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=update

func (r *KymaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.V(log.DebugLevel).Info("Kyma reconciliation started")

	ctx = adapter.ContextWithRecorder(ctx, r.EventRecorder)

	kyma := &v1beta2.Kyma{}
	if err := r.Get(ctx, req.NamespacedName, kyma); err != nil {
		if util.IsNotFound(err) {
			logger.V(log.DebugLevel).Info(fmt.Sprintf("Kyma %s not found, probably already deleted",
				req.NamespacedName))
			return ctrl.Result{Requeue: false}, nil
		}
		r.Metrics.RecordRequeueReason(metrics.KymaRetrieval, queue.UnexpectedRequeue)
		return ctrl.Result{}, fmt.Errorf("KymaController: %w", err)
	}

	status.InitConditions(kyma, false, false)

	if kyma.SkipReconciliation() {
		logger.V(log.DebugLevel).Info("skipping reconciliation for Kyma: " + kyma.Name)
		return ctrl.Result{RequeueAfter: r.RequeueIntervals.Success}, nil
	}

	return r.reconcile(ctx, kyma)
}

func (r *KymaReconciler) reconcile(ctx context.Context, kyma *v1beta2.Kyma) (ctrl.Result, error) {
	if !kyma.DeletionTimestamp.IsZero() && kyma.Status.State != shared.StateDeleting {
		if err := r.updateStatus(ctx, kyma, shared.StateDeleting, "waiting for modules to be deleted"); err != nil {
			r.Metrics.RecordRequeueReason(metrics.StatusUpdateToDeleting, queue.UnexpectedRequeue)
			return r.requeueWithError(ctx, kyma,
				fmt.Errorf("could not update kyma status after triggering deletion: %w", err))
		}
		r.Metrics.RecordRequeueReason(metrics.StatusUpdateToDeleting, queue.IntendedRequeue)
		return ctrl.Result{Requeue: true}, nil
	}

	if needsUpdate := kyma.EnsureLabelsAndFinalizers(); needsUpdate {
		if err := r.Update(ctx, kyma); err != nil {
			r.Metrics.RecordRequeueReason(metrics.LabelsAndFinalizersUpdate, queue.UnexpectedRequeue)
			return r.requeueWithError(ctx, kyma, fmt.Errorf("failed to update kyma after finalizer check: %w", err))
		}
		r.Metrics.RecordRequeueReason(metrics.LabelsAndFinalizersUpdate, queue.IntendedRequeue)
		return ctrl.Result{Requeue: true}, nil
	}

	result, err := r.processKymaState(ctx, kyma)
	if err != nil {
		r.Metrics.RecordRequeueReason(metrics.ProcessingKymaState, queue.UnexpectedRequeue)
		return ctrl.Result{}, err
	}

	return result, err
}

func (r *KymaReconciler) requeueWithError(ctx context.Context, kyma *v1beta2.Kyma, err error) (ctrl.Result, error) {
	updateErr := r.updateStatusWithError(ctx, kyma, err)
	if updateErr == nil {
		r.enqueueWarningEvent(kyma, moduleReconciliationError, err)
	}

	return ctrl.Result{Requeue: true}, updateErr
}

func (r *KymaReconciler) enqueueWarningEvent(kyma *v1beta2.Kyma, reason EventReasonError, err error) {
	r.Event(kyma, "Warning", string(reason), err.Error())
}

func (r *KymaReconciler) enqueueNormalEvent(kyma *v1beta2.Kyma, reason EventReasonInfo, message string) {
	r.Event(kyma, "Normal", string(reason), message)
}

func (r *KymaReconciler) processKymaState(ctx context.Context, kyma *v1beta2.Kyma) (ctrl.Result, error) {
	switch kyma.Status.State {
	case "":
		return r.handleInitialState(ctx, kyma)
	case shared.StateProcessing:
		return r.handleProcessingState(ctx, kyma)
	case shared.StateDeleting:
		return r.handleDeletingState(ctx, kyma)
	case shared.StateError:
		return r.handleProcessingState(ctx, kyma)
	case shared.StateReady, shared.StateWarning:
		return r.handleProcessingState(ctx, kyma)
	}

	return ctrl.Result{Requeue: false}, nil
}

func (r *KymaReconciler) handleInitialState(ctx context.Context, kyma *v1beta2.Kyma) (ctrl.Result, error) {
	const msg = "started processing"
	if err := r.updateStatus(ctx, kyma, shared.StateProcessing, msg); err != nil {
		r.Metrics.RecordRequeueReason(metrics.InitialStateHandling, queue.UnexpectedRequeue)
		return ctrl.Result{}, err
	}
	r.enqueueNormalEvent(kyma, updateStatus, msg)
	r.Metrics.RecordRequeueReason(metrics.InitialStateHandling, queue.IntendedRequeue)
	return ctrl.Result{Requeue: true}, nil
}

func (r *KymaReconciler) handleProcessingState(ctx context.Context, kyma *v1beta2.Kyma) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	var errGroup errgroup.Group
	errGroup.Go(func() error {
		err := r.reconcileManifests(ctx, kyma)
		if err != nil {
			r.Metrics.RecordRequeueReason(metrics.ReconcileManifests, queue.UnexpectedRequeue)
			return fmt.Errorf("could not reconciling manifest: %w", err)
		}
		if kyma.AllModulesReady() {
			kyma.UpdateCondition(v1beta2.ConditionTypeModules, apimetav1.ConditionTrue)
		} else {
			kyma.UpdateCondition(v1beta2.ConditionTypeModules, apimetav1.ConditionFalse)
		}
		return nil
	})

	if err := errGroup.Wait(); err != nil {
		return r.requeueWithError(ctx, kyma, err)
	}

	state := kyma.DetermineState()
	requeueInterval := queue.DetermineRequeueInterval(state, r.RequeueIntervals)
	if state == shared.StateReady {
		const msg = "kyma is ready"
		if kyma.Status.State != shared.StateReady {
			logger.Info(msg)
		}
		return ctrl.Result{RequeueAfter: requeueInterval}, r.updateStatus(ctx, kyma, state, msg)
	}

	return ctrl.Result{RequeueAfter: requeueInterval},
		r.updateStatus(ctx, kyma, state, "waiting for all modules to become ready")
}

func (r *KymaReconciler) handleDeletingState(ctx context.Context, kyma *v1beta2.Kyma) (ctrl.Result, error) {

	err := r.cleanupManifestCRs(ctx, kyma)
	if err != nil {
		r.enqueueWarningEvent(kyma, deletionError, err)
		r.Metrics.RecordRequeueReason(metrics.CleanupManifestCrs, queue.UnexpectedRequeue)
		return ctrl.Result{}, err
	}

	r.Metrics.CleanupMetrics(kyma.Name)

	controllerutil.RemoveFinalizer(kyma, shared.KymaFinalizer)

	if err := r.updateKyma(ctx, kyma); err != nil {
		r.Metrics.RecordRequeueReason(metrics.KymaDeletion, queue.UnexpectedRequeue)
		return ctrl.Result{}, err
	}
	r.Metrics.RecordRequeueReason(metrics.KymaDeletion, queue.IntendedRequeue)
	return ctrl.Result{Requeue: true}, nil
}

func (r *KymaReconciler) cleanupManifestCRs(ctx context.Context, kyma *v1beta2.Kyma) error {
	relatedManifests, err := r.getRelatedManifestCRs(ctx, kyma)
	if err != nil {
		return fmt.Errorf("error while trying to get manifests: %w", err)
	}

	if r.relatedManifestCRsAreDeleted(relatedManifests) {
		return nil
	}

	if err = r.deleteManifests(ctx, relatedManifests); err != nil {
		return fmt.Errorf("error while trying to delete manifests: %w", err)
	}
	return ErrManifestsStillExist
}

func (r *KymaReconciler) deleteManifests(ctx context.Context, manifests []v1beta2.Manifest) error {
	for i := range manifests {
		if err := r.Delete(ctx, &manifests[i]); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("error while trying to delete manifest: %w", err)
		}
	}
	return nil
}

func (r *KymaReconciler) getRelatedManifestCRs(ctx context.Context, kyma *v1beta2.Kyma) ([]v1beta2.Manifest, error) {
	manifestList := &v1beta2.ManifestList{}
	labelSelector := k8slabels.SelectorFromSet(k8slabels.Set{shared.KymaName: kyma.Name})
	if err := r.List(ctx, manifestList,
		&client.ListOptions{LabelSelector: labelSelector}); client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("failed to get related manifests, %w", err)
	}

	return manifestList.Items, nil
}

func (r *KymaReconciler) relatedManifestCRsAreDeleted(manifests []v1beta2.Manifest) bool {
	return len(manifests) == 0
}

func (r *KymaReconciler) removeAllFinalizers(kyma *v1beta2.Kyma) {
	for _, finalizer := range kyma.Finalizers {
		controllerutil.RemoveFinalizer(kyma, finalizer)
	}
}

func (r *KymaReconciler) updateKyma(ctx context.Context, kyma *v1beta2.Kyma) error {
	if err := r.Update(ctx, kyma); err != nil {
		err = fmt.Errorf("error while updating kyma during deletion: %w", err)
		r.enqueueWarningEvent(kyma, deletionError, err)
		return err
	}

	return nil
}

func (r *KymaReconciler) reconcileManifests(ctx context.Context, kyma *v1beta2.Kyma) error {
	modules, err := r.GenerateModulesFromTemplate(ctx, kyma)
	if err != nil {
		return fmt.Errorf("error while fetching modules during processing: %w", err)
	}

	runner := sync.New(r)

	if err := runner.ReconcileManifests(ctx, kyma, modules); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	runner.SyncModuleStatus(ctx, kyma, modules, r.Metrics)
	// If module get removed from kyma, the module deletion happens here.
	if err := r.DeleteNoLongerExistingModules(ctx, kyma); err != nil {
		return fmt.Errorf("error while syncing conditions during deleting non exists modules: %w", err)
	}
	return nil
}

func (r *KymaReconciler) syncModuleCatalog(ctx context.Context, kyma *v1beta2.Kyma) error {
	moduleTemplateList := &v1beta2.ModuleTemplateList{}
	if err := r.List(ctx, moduleTemplateList, &client.ListOptions{}); err != nil {
		return fmt.Errorf("could not aggregate module templates for module catalog sync: %w", err)
	}

	var modulesToSync []v1beta2.ModuleTemplate
	for _, mt := range moduleTemplateList.Items {
		if mt.SyncEnabled(kyma.IsBeta(), kyma.IsInternal()) {
			modulesToSync = append(modulesToSync, mt)
		}
	}

	if err := remote.NewRemoteCatalogFromKyma(r.RemoteSyncNamespace).CreateOrUpdate(ctx, modulesToSync); err != nil {
		return fmt.Errorf("could not synchronize remote module catalog: %w", err)
	}

	return nil
}

func (r *KymaReconciler) updateStatus(ctx context.Context, kyma *v1beta2.Kyma,
	state shared.State, message string,
) error {
	if err := status.Helper(r).UpdateStatusForExistingModules(ctx, kyma, state, message); err != nil {
		return fmt.Errorf("error while updating status to %s because of %s: %w", state, message, err)
	}
	return nil
}

func (r *KymaReconciler) updateStatusWithError(ctx context.Context, kyma *v1beta2.Kyma, err error) error {
	if err := status.Helper(r).UpdateStatusForExistingModules(ctx, kyma, shared.StateError, err.Error()); err != nil {
		return fmt.Errorf("error while updating status to %s: %w", shared.StateError, err)
	}

	return nil
}

func (r *KymaReconciler) GenerateModulesFromTemplate(ctx context.Context, kyma *v1beta2.Kyma) (common.Modules, error) {
	lookup := templatelookup.NewTemplateLookup(client.Reader(r), r.DescriptorProvider)
	templates := lookup.GetRegularTemplates(ctx, kyma)
	for _, template := range templates {
		if template.Err != nil {
			r.enqueueWarningEvent(kyma, moduleReconciliationError, template.Err)
		}
	}
	parser := parse.NewParser(r.Client, r.DescriptorProvider, false, r.RemoteSyncNamespace)
	return parser.GenerateModulesFromTemplates(kyma, templates), nil
}

func (r *KymaReconciler) DeleteNoLongerExistingModules(ctx context.Context, kyma *v1beta2.Kyma) error {
	moduleStatus := kyma.GetNoLongerExistingModuleStatus()
	var err error
	if len(moduleStatus) == 0 {
		return nil
	}
	for i := range moduleStatus {
		moduleStatus := moduleStatus[i]
		if moduleStatus.Manifest == nil {
			continue
		}
		err = r.deleteManifest(ctx, moduleStatus.Manifest)
	}

	if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("error deleting module %w", err)
	}
	return nil
}

func (r *KymaReconciler) deleteManifest(ctx context.Context, trackedManifest *v1beta2.TrackingObject) error {
	manifest := apimetav1.PartialObjectMetadata{}
	manifest.SetGroupVersionKind(trackedManifest.GroupVersionKind())
	manifest.SetNamespace(trackedManifest.GetNamespace())
	manifest.SetName(trackedManifest.GetName())

	err := r.Delete(ctx, &manifest, &client.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed delete manifest crd: %w", err)
	}
	return nil
}

func (r *KymaReconciler) UpdateMetrics(ctx context.Context, kyma *v1beta2.Kyma) {
	if err := r.Metrics.UpdateAll(kyma); err != nil {
		if metrics.IsMissingMetricsAnnotationOrLabel(err) {
			r.enqueueWarningEvent(kyma, metricsError, err)
		}
		logf.FromContext(ctx).V(log.DebugLevel).Info(fmt.Sprintf("error occurred while updating all metrics: %s", err))
	}
}

func (r *KymaReconciler) IsKymaManaged() bool {
	return r.IsManagedKyma
}
