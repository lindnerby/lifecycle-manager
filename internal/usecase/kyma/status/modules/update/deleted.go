package update

import (
	"context"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/internal/common/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/common/client"
	"github.com/kyma-project/lifecycle-manager/pkg/util"
)

type MetricsCleanupFunc func(kymaName, moduleName string)

type ManifestClient interface {
	Get(objectKey client.ObjectKey) (v1beta2.Manifest, error)
}

type UpdateStatusModulesDeletedUC struct {
	manifestClient ManifestClient
	cleanupMetrics MetricsCleanupFunc
}

func NewUpdateStatusModulesDeletedUC(cleanupMetrics MetricsCleanupFunc, manifestClient ManifestClient) *UpdateStatusModulesDeletedUC {
	return &UpdateStatusModulesDeletedUC{
		manifestClient: manifestClient,
		cleanupMetrics: cleanupMetrics,
	}
}

func (uc *UpdateStatusModulesDeletedUC) Execute(ctx context.Context, kyma *v1beta2.Kyma) error {
	moduleStatuses := kyma.GetModuleStatusMap()
	moduleStatusesToBeDeletedFromKymaStatus := kyma.GetNoLongerExistingModuleStatus()

	for _, moduleStatus := range moduleStatusesToBeDeletedFromKymaStatus {
		if moduleStatus.Manifest == nil {
			uc.cleanupMetrics(kyma.Name, moduleStatus.Name)
			delete(moduleStatuses, moduleStatus.Name)
			continue
		}
		manifestName := moduleStatus.GetManifestName()
		if manifestName.Name == "" {
			return errors.ErrModulesManifestNameEmpty
		}
		if manifestName.Namespace == "" {
			return errors.ErrModulesManifestNamespaceEmpty
		}

		manifest, err := uc.manifestClient.Get(manifestName)
		if util.IsNotFound(err) {
			uc.cleanupMetrics(kyma.Name, moduleStatus.Name)
			delete(moduleStatuses, moduleStatus.Name)
		} else {
			moduleStatus.State = manifest.Status.State
		}
	}
	kyma.Status.Modules = convertToNewModuleStatus(moduleStatuses)

	return nil
}

func convertToNewModuleStatus(moduleStatusMap map[string]*v1beta2.ModuleStatus) []v1beta2.ModuleStatus {
	newModuleStatus := make([]v1beta2.ModuleStatus, 0)
	for _, moduleStatus := range moduleStatusMap {
		newModuleStatus = append(newModuleStatus, *moduleStatus)
	}
	return newModuleStatus
}
