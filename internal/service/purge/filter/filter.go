package filter

import (
	"context"
	"fmt"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	errors "github.com/kyma-project/lifecycle-manager/internal/errors/purge"
	"github.com/kyma-project/lifecycle-manager/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"time"

	"github.com/kyma-project/lifecycle-manager/pkg/log"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
)

type RequestFilter struct {
	client  client.Client
	timeout time.Duration
}

func (p *RequestFilter) ToResource(ctx context.Context, req ctrl.Request) (*v1beta2.Kyma, error) {
	debugLog := logf.FromContext(ctx).V(log.DebugLevel)

	kyma := &v1beta2.Kyma{}
	if err := p.client.Get(ctx, req.NamespacedName, kyma); err != nil {
		if util.IsNotFound(err) {
			debugLog.Info(fmt.Sprintf("Kyma %s not found, probably already deleted", kyma.GetName()))
			return nil, errors.ErrKymaNotFound
		}
		return nil, errors.ErrClientGetKyma
	}

	if kyma.HasDeletionTimestamp() {
		requeueAfter := kyma.CalculateRequeueAfterTime(p.timeout)
		if requeueAfter != 0 {
			return nil, errors.ErrPurgeNotDue
		}
	}

	if controllerutil.AddFinalizer(kyma, shared.PurgeFinalizer) {
		if err := p.client.Update(ctx, kyma); err != nil {
			debugLog.Info(fmt.Sprintf("Failed setting purge finalizer for Kyma %s: %s", kyma.GetName(), err))
			return nil, errors.ErrFinalizerSet
		}
	}

	return kyma, nil
}
