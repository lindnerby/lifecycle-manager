package usecase

import (
	"context"
	"fmt"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/crd"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/pkg/queue"
	"github.com/kyma-project/lifecycle-manager/pkg/remote"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SyncRemoteCrds struct {
	*metrics.KymaMetrics
	service crd.SyncRemoteCrdService
}

func (s *SyncRemoteCrds) Execute(ctx context.Context, kyma v1beta2.Kyma) error {
	syncContext, err := remote.SyncContextFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to get syncContext: %w", err)
	}

	s.service = crd.SyncRemoteCrdService{}
	updateRequired, err := remote.SyncCrdsAndUpdateKymaAnnotations(
		ctx, kyma, syncContext.RuntimeClient, syncContext.ControlPlaneClient)
	if err != nil {
		return false, err
	}

	kymaCrdUpdated, err := s.SyncRemoteCrdService.PatchCrdsAndUpdateKymaAnnotations(ctx, kyma, shared.KymaKind)
	if client.IgnoreNotFound(err) != nil {
		return false, fmt.Errorf("failed to fetch Kyma CRDs and update its Kyma annotations: %w", err)
	}

	moduleTemplateCrdUpdated, err := c.patchCrdsAndUpdateKymaAnnotations(ctx, kyma, shared.ModuleTemplateKind)
	if client.IgnoreNotFound(err) != nil {
		return false, fmt.Errorf("failed to fetch ModuleTemplate CRDs and update its Kyma annotations: %w", err)
	}

	updateKymaRequired, err := r.syncCrdsAndUpdateKymaAnnotations(ctx, kyma)
	if err != nil {
		r.Metrics.RecordRequeueReason(metrics.CrdsSync, queue.UnexpectedRequeue)
		return r.requeueWithError(ctx, kyma, fmt.Errorf("could not sync CRDs: %w", err))
	}
	if kymaCrdUpdated || moduleTemplateCrdUpdated {
		if err := r.Update(ctx, kyma); err != nil {
			r.Metrics.RecordRequeueReason(metrics.CrdAnnotationsUpdate, queue.UnexpectedRequeue)
			return r.requeueWithError(ctx, kyma, fmt.Errorf("could not update kyma annotations: %w", err))
		}
		r.Metrics.RecordRequeueReason(metrics.CrdAnnotationsUpdate, queue.IntendedRequeue)
		return ctrl.Result{Requeue: true}, nil
	}

	return nil
}
