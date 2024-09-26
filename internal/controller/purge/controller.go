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

package purge

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/event"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/internal/remote"
	"github.com/kyma-project/lifecycle-manager/pkg/log"
	"github.com/kyma-project/lifecycle-manager/pkg/matcher"
	"github.com/kyma-project/lifecycle-manager/pkg/status"
	"github.com/kyma-project/lifecycle-manager/pkg/util"
)

const (
	setFinalizerFailure    event.Reason = "SettingPurgeFinalizerFailed"
	removeFinalizerFailure event.Reason = "RemovingPurgeFinalizerFailed"
)

type Reconciler struct {
	client.Client
	event.Event
	SkrContextFactory remote.SkrContextProvider
	SkipCRDs          matcher.CRDMatcherFunc
	IsManagedKyma     bool
	Metrics           *metrics.PurgeMetrics
	requestFilter     RequestFilter
	skrCleanupHandler Handler
}

type RequestFilter interface {
	ToResource(ctx context.Context, req ctrl.Request) (*v1beta2.Kyma, error)
}

type Handler interface {
	DoCleanup(ctx context.Context, kyma *v1beta2.Kyma, remoteClient client.Client) (ctrl.Result, error)
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	logger.V(log.DebugLevel).Info("Purge reconciliation started")

	kyma, err := r.requestFilter.ToResource(ctx, req)
	if err != nil {
		// TODO handle different cases
		return ctrl.Result{}, err
	}

	err = r.SkrContextFactory.Init(ctx, kyma.GetNamespacedName())
	if err != nil {
		return r.handleSkrNotFoundError(ctx, kyma, err)
	}

	skrContext, err := r.SkrContextFactory.Get(kyma.GetNamespacedName())
	if err != nil {
		return r.handleSkrNotFoundError(ctx, kyma, err)
	}

	return r.skrCleanupHandler.DoCleanup(ctx, kyma, skrContext.Client)
}

func (r *Reconciler) UpdateStatus(ctx context.Context, kyma *v1beta2.Kyma, state shared.State, message string) error {
	if err := status.Helper(r).UpdateStatusForExistingModules(ctx, kyma, state, message); err != nil {
		return fmt.Errorf("failed updating status to %s because of %s: %w", state, message, err)
	}
	return nil
}

func (r *Reconciler) UpdateMetrics(_ context.Context, _ *v1beta2.Kyma) {}

func (r *Reconciler) IsKymaManaged() bool {
	return r.IsManagedKyma
}

func (r *Reconciler) handleRemovingPurgeFinalizerFailedError(ctx context.Context, kyma *v1beta2.Kyma, err error) (ctrl.Result, error) {
	logf.FromContext(ctx).Error(err, fmt.Sprintf("Failed removing purge finalizer from Kyma %s/%s", kyma.GetNamespace(), kyma.GetName()))
	r.Event.Warning(kyma, removeFinalizerFailure, err)
	r.Metrics.SetPurgeError(ctx, kyma, metrics.ErrPurgeFinalizerRemoval)
	return ctrl.Result{}, err
}

func (r *Reconciler) handleSkrNotFoundError(ctx context.Context, kyma *v1beta2.Kyma, err error) (ctrl.Result, error) {
	if util.IsNotFound(err) {
		dropped, err := r.dropPurgeFinalizer(ctx, kyma)
		if err != nil {
			return r.handleRemovingPurgeFinalizerFailedError(ctx, kyma, err)
		}
		if dropped {
			logf.FromContext(ctx).Info("Removed purge finalizer for Kyma " + kyma.GetName())
		}
		r.Metrics.DeletePurgeError(ctx, kyma, metrics.ErrPurgeFinalizerRemoval)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, fmt.Errorf("failed getting remote client for Kyma %s: %w", kyma.GetName(), err)
}

func (r *Reconciler) handleCleanupError(ctx context.Context, kyma *v1beta2.Kyma, err error) (ctrl.Result, error) {
	logf.FromContext(ctx).Error(err, "failed purge cleanup for Kyma "+kyma.GetName())
	r.Metrics.SetPurgeError(ctx, kyma, metrics.ErrCleanup)
	return ctrl.Result{}, err
}

func handlePurgeNotDue(logger logr.Logger, kyma *v1beta2.Kyma, requeueAfter time.Duration) (ctrl.Result, error) {
	logger.Info(fmt.Sprintf("Purge reconciliation for Kyma %s will be requeued after %s", kyma.GetName(), requeueAfter))
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}
