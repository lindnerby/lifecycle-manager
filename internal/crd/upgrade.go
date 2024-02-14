package crd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/crd/cache"
)

const (
	kcp = "KCP"
	skr = "SKR"
)

type Client interface {
	client.Reader
	client.Writer
}

type CachedUpgradeClient struct {
	kcpClient     Client
	runtimeClient Client
	crdCache      *cache.CrdCache
}

func NewCachedUpgradeClient() *CachedUpgradeClient {

}

func (c *CachedUpgradeClient) SyncCrds(ctx context.Context, kyma *v1beta2.Kyma) (bool, error) {
	kymaCrdUpdated, err := c.fetchCrdsAndUpdateKymaAnnotations(ctx, kyma, shared.KymaKind)
	if client.IgnoreNotFound(err) != nil {
		return false, fmt.Errorf("failed to fetch Kyma CRDs and update its Kyma annotations: %w", err)
	}

	moduleTemplateCrdUpdated, err := c.fetchCrdsAndUpdateKymaAnnotations(ctx, kyma, shared.ModuleTemplateKind)
	if client.IgnoreNotFound(err) != nil {
		return false, fmt.Errorf("failed to fetch ModuleTemplate CRDs and update its Kyma annotations: %w", err)
	}

	return kymaCrdUpdated || moduleTemplateCrdUpdated, nil
}

func (c *CachedUpgradeClient) fetchCrdsAndUpdateKymaAnnotations(ctx context.Context, kyma *v1beta2.Kyma, kind shared.Kind) (bool, error) {
	crdName := GetName(kind)
	kcpCrd, skrCrd, err := c.fetchCrds(ctx, crdName)
	if err != nil {
		return false, err
	}
	crdUpdated, err := c.updateRemoteCRD(ctx, kyma, skrCrd, kcpCrd)
	if err != nil {
		return false, err
	}
	if crdUpdated {
		err = c.runtimeClient.Get(ctx, client.ObjectKey{Name: string(crdName)}, skrCrd)
		if err != nil {
			return false, fmt.Errorf("failed to get SKR CRD: %w", err)
		}
		updateKymaAnnotations(kyma, kcpCrd, skrCrd)
	}

	return crdUpdated, nil
}

func (c *CachedUpgradeClient) fetchCrds(ctx context.Context, crdName Name) (*apiextensionsv1.CustomResourceDefinition, *apiextensionsv1.CustomResourceDefinition, error) {
	crd, ok := c.crdCache.Get(crdName)
	if !ok {
		crd = apiextensionsv1.CustomResourceDefinition{}
		err := c.kcpClient.Get(ctx, client.ObjectKey{Name: string(crdName)}, &crd)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch CRDs from kcp: %w", err)
		}
		c.crdCache.Add(crdName, crd)
	}

	crdFromRuntime := &apiextensionsv1.CustomResourceDefinition{}
	err := c.runtimeClient.Get(ctx, client.ObjectKey{Name: crdName}, crdFromRuntime)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch CRDs from runtime: %w", err)
	}

	return &crd, crdFromRuntime, nil
}

func (c *CachedUpgradeClient) PatchCRD(ctx context.Context, crd *apiextensionsv1.CustomResourceDefinition) error {
	crdToApply := &apiextensionsv1.CustomResourceDefinition{}
	crdToApply.SetGroupVersionKind(crd.GroupVersionKind())
	crdToApply.SetName(crd.Name)
	crdToApply.Spec = crd.Spec
	crdToApply.Spec.Conversion.Strategy = apiextensionsv1.NoneConverter
	crdToApply.Spec.Conversion.Webhook = nil
	err := c.runtimeClient.Patch(ctx, crdToApply,
		client.Apply,
		client.ForceOwnership,
		client.FieldOwner(shared.OperatorName))
	if err != nil {
		return fmt.Errorf("failed to patch CRD: %w", err)
	}
	return nil
}

func (c *CachedUpgradeClient) updateRemoteCRD(ctx context.Context, kyma *v1beta2.Kyma, crdFromRuntime *apiextensionsv1.CustomResourceDefinition, kcpCrd *apiextensionsv1.CustomResourceDefinition) (bool, error) {
	if ShouldPatchRemoteCRD(crdFromRuntime, kcpCrd, kyma) {
		err := c.PatchCRD(ctx, kcpCrd)
		if err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

func ShouldPatchRemoteCRD(skrCrd *apiextensionsv1.CustomResourceDefinition, kcpCrd *apiextensionsv1.CustomResourceDefinition, kyma *v1beta2.Kyma) bool {
	return kyma.Annotations[kcpAnnotation(kcpCrd)] != strconv.FormatInt(kcpCrd.Generation, 10) ||
		kyma.Annotations[skrAnnotation(skrCrd)] != strconv.FormatInt(skrCrd.Generation, 10)
}

func updateKymaAnnotations(kyma *v1beta2.Kyma, kcpCrd, skrCrd *apiextensionsv1.CustomResourceDefinition) {
	if kyma.Annotations == nil {
		kyma.Annotations = make(map[string]string)
	}

	kyma.Annotations[kcpAnnotation(kcpCrd)] = strconv.FormatInt(kcpCrd.Generation, 10)
	kyma.Annotations[skrAnnotation(skrCrd)] = strconv.FormatInt(skrCrd.Generation, 10)
}

func kcpAnnotation(crd *apiextensionsv1.CustomResourceDefinition) string {
	return fmt.Sprintf("%s-kcp-crd-generation", strings.ToLower(crd.Spec.Names.Kind))
}

func skrAnnotation(crd *apiextensionsv1.CustomResourceDefinition) string {
	return fmt.Sprintf("%s-skr-crd-generation", strings.ToLower(crd.Spec.Names.Kind))
}

func ContainsLatestVersion(crdFromRuntime *apiextensionsv1.CustomResourceDefinition, latestVersion string) bool {
	for _, version := range crdFromRuntime.Spec.Versions {
		if latestVersion == version.Name {
			return true
		}
	}
	return false
}

func CRDNotFoundErr(err error) bool {
	groupErr := &discovery.ErrGroupDiscoveryFailed{}
	if !errors.As(err, &groupErr) {
		return false
	}
	for _, err := range groupErr.Groups {
		if cannotFoundResource(err) {
			return true
		}
	}
	return false
}

func cannotFoundResource(err error) bool {
	var apiStatusErr apierrors.APIStatus
	if ok := errors.As(err, &apiStatusErr); ok && apiStatusErr.Status().Details != nil {
		for _, cause := range apiStatusErr.Status().Details.Causes {
			if cause.Type == apimetav1.CauseTypeUnexpectedServerResponse &&
				strings.Contains(cause.Message, "not found") {
				return true
			}
		}
	}
	return false
}
