package kyma

import (
	"context"
	machineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/descriptor/provider"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/internal/remote"
	"github.com/kyma-project/lifecycle-manager/pkg/module/common"
	"github.com/kyma-project/lifecycle-manager/pkg/queue"
	"github.com/kyma-project/lifecycle-manager/pkg/templatelookup"
	"github.com/kyma-project/lifecycle-manager/pkg/watcher"
)

type EventReason = string
type Object = machineryruntime.Object

type Event interface {
	Normal(object Object, reason EventReason, msg string)
	Warning(object Object, reason EventReason, err error)
}

type UpdateStatusModules interface {
	Execute(ctx context.Context, kyma *v1beta2.Kyma, modules common.Modules) error
}

type SyncCrds interface {
	Execute(ctx context.Context, kyma *v1beta2.Kyma) (bool, error)
}

type Reconciler struct {
	client.Client
	event Event
	queue.RequeueIntervals
	SkrContextFactory     remote.SkrContextProvider
	DescriptorProvider    *provider.CachedDescriptorProvider
	syncRemoteCrdsUC      SyncCrds
	updateStatusModulesUC UpdateStatusModules
	SKRWebhookManager     *watcher.SKRWebhookManifestManager
	InKCPMode             bool
	RemoteSyncNamespace   string
	IsManagedKyma         bool
	Metrics               *metrics.KymaMetrics
	RemoteCatalog         *remote.RemoteCatalog
	TemplateLookup        *templatelookup.TemplateLookup
}

func NewReconciler(client client.Client,
	event Event,
	requeueIntervals queue.RequeueIntervals,
	skrContextFactory remote.SkrContextProvider,
	descriptorProvider *provider.CachedDescriptorProvider,
	syncRemoteCrdsUC SyncCrds,
	updateStatusModulesUC UpdateStatusModules,
	skrWebhookManager *watcher.SKRWebhookManifestManager,
	metrics *metrics.KymaMetrics,
	remoteCatalog *remote.RemoteCatalog,
	templateLookup *templatelookup.TemplateLookup) *Reconciler {
	return &Reconciler{
		Client:                client,
		event:                 event,
		RequeueIntervals:      requeueIntervals,
		SkrContextFactory:     skrContextFactory,
		DescriptorProvider:    descriptorProvider,
		syncRemoteCrdsUC:      syncRemoteCrdsUC,
		updateStatusModulesUC: updateStatusModulesUC,
		SKRWebhookManager:     skrWebhookManager,
		Metrics:               metrics,
		RemoteCatalog:         remoteCatalog,
		TemplateLookup:        templateLookup,
	}
}
