package purge

import (
	"context"
	"fmt"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/pkg/matcher"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

type SkrCleanup struct {
}

func (r *SkrCleanup) DoCleanup(ctx context.Context, kyma *v1beta2.Kyma, skrClient client.Client) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)
	start := time.Now()
	r.Metrics.UpdatePurgeCount()

	handledResources, err := r.performCleanup(ctx, skrClient)
	if len(handledResources) > 0 {
		logger.Info(fmt.Sprintf("Removed all finalizers for Kyma %s related resources %s", kyma.GetName(), strings.Join(handledResources, ", ")))
	}
	if err != nil {
		return r.handleCleanupError(ctx, kyma, err)
	}
	r.Metrics.DeletePurgeError(ctx, kyma, metrics.ErrCleanup)

	dropped, err := r.dropPurgeFinalizer(ctx, kyma)
	if dropped {
		logger.Info("Removed purge finalizer for Kyma " + kyma.GetName())
	}
	if err != nil {
		return r.handleRemovingPurgeFinalizerFailedError(ctx, kyma, err)
	}
	r.Metrics.DeletePurgeError(ctx, kyma, metrics.ErrPurgeFinalizerRemoval)

	r.Metrics.UpdatePurgeTime(time.Since(start))
	return ctrl.Result{}, nil
}

func (r *SkrCleanup) performCleanup(ctx context.Context, remoteClient client.Client) ([]string, error) {
	crdList := apiextensionsv1.CustomResourceDefinitionList{}
	if err := remoteClient.List(ctx, &crdList); err != nil {
		return nil, fmt.Errorf("failed fetching CRDs from remote cluster: %w", err)
	}

	var allHandledResources []string
	for _, crd := range crdList.Items {
		if shouldSkip(crd, r.SkipCRDs) {
			continue
		}

		staleResources, err := getAllRemainingCRs(ctx, remoteClient, crd)
		if err != nil {
			return allHandledResources, fmt.Errorf("failed fetching stale resources from remote cluster: %w", err)
		}

		handledResources, err := dropFinalizers(ctx, remoteClient, staleResources)
		if err != nil {
			return allHandledResources, fmt.Errorf("failed removing finalizers from stale resources: %w", err)
		}

		allHandledResources = append(allHandledResources, handledResources...)
	}

	return allHandledResources, nil
}

func shouldSkip(crd apiextensionsv1.CustomResourceDefinition, matcher matcher.CRDMatcherFunc) bool {
	if crd.Spec.Group == v1beta2.GroupVersion.Group && crd.Spec.Names.Kind == string(shared.KymaKind) {
		return true
	}
	return matcher(crd)
}

func getAllRemainingCRs(ctx context.Context, remoteClient client.Client,
	crd apiextensionsv1.CustomResourceDefinition,
) (unstructured.UnstructuredList, error) {
	staleResources := unstructured.UnstructuredList{}

	// Since there are multiple possible versions, we are choosing storage version
	var gvk schema.GroupVersionKind
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			gvk = schema.GroupVersionKind{
				Group:   crd.Spec.Group,
				Kind:    crd.Spec.Names.Kind,
				Version: version.Name,
			}
			break
		}
	}
	staleResources.SetGroupVersionKind(gvk)

	if err := remoteClient.List(ctx, &staleResources); err != nil {
		return unstructured.UnstructuredList{}, fmt.Errorf("failed fetching resources: %w", err)
	}

	return staleResources, nil
}

func (r *SkrCleanup) dropPurgeFinalizer(ctx context.Context, kyma *v1beta2.Kyma) (bool, error) {
	if controllerutil.RemoveFinalizer(kyma, shared.PurgeFinalizer) {
		if err := r.Update(ctx, kyma); err != nil {
			return false, fmt.Errorf("failed updating object: %w", err)
		}
		return true, nil
	}
	return false, nil
}

func dropFinalizers(ctx context.Context, remoteClient client.Client,
	staleResources unstructured.UnstructuredList,
) ([]string, error) {
	handledResources := make([]string, 0, len(staleResources.Items))
	for index := range staleResources.Items {
		resource := staleResources.Items[index]
		resource.SetFinalizers(nil)
		if err := remoteClient.Update(ctx, &resource); err != nil {
			return handledResources, fmt.Errorf("failed updating resource: %w", err)
		}
		handledResources = append(handledResources, fmt.Sprintf("%s/%s", resource.GetNamespace(), resource.GetName()))
	}
	return handledResources, nil
}
