package modules

import (
	"context"
	"errors"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/pkg/templatelookup"
	"github.com/kyma-project/lifecycle-manager/pkg/templatelookup/moduletemplateinfolookup"
	"github.com/kyma-project/lifecycle-manager/pkg/util"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/internal/pkg/metrics"
	"github.com/kyma-project/lifecycle-manager/pkg/module/common"
)

type UpdateStatusModulesExisting interface {
	Execute(ctx context.Context, kyma *v1beta2.Kyma, modules common.Modules) error
}

type UpdateStatusModulesDeleted interface {
	Execute(ctx context.Context, kyma *v1beta2.Kyma)
}

type UpdateStatusModulesUC struct {
	updateExistingStatusModulesUC UpdateStatusModulesExisting
	updateDeletedStatusModulesUC  UpdateStatusModulesDeleted
}

func NewUpdateStatusModulesUC() *UpdateStatusModulesUC {
	return &UpdateStatusModulesUC{}
}

func (uc *UpdateStatusModulesUC) Execute(ctx context.Context, kyma *v1beta2.Kyma, modules common.Modules) error {
	err := uc.updateExistingStatusModulesUC.Execute(ctx, kyma, modules)

	updateModuleStatusFromExistingModules(kyma, modules)
	deleteNoLongerExistingModuleStatus(ctx, kyma, r.getModule, kymaMetrics.RemoveModuleStateMetrics)

	return nil
}

func updateModuleStatusFromExistingModules(kyma *v1beta2.Kyma, modules common.Modules) {
	currentStatusModules := kyma.GetModuleStatusMap()

	for _, module := range modules {
		currentModuleStatus, exists := currentStatusModules[module.Name]
		latestModuleStatus := generateModuleStatus(module, currentModuleStatus)
		if exists {
			*currentModuleStatus = latestModuleStatus
		} else {
			kyma.Status.Modules = append(kyma.Status.Modules, latestModuleStatus)
		}
	}
}

func generateModuleStatus(module *common.Module, existStatus *v1beta2.ModuleStatus) v1beta2.ModuleStatus {
	if module.Template.Err != nil {
		return generateModuleStatusFromError(module, existStatus)
	}

	manifestObject := module.Manifest
	manifestAPIVersion, manifestKind := manifestObject.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	templateAPIVersion, templateKind := module.Template.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
	var moduleResource *v1beta2.TrackingObject
	if manifestObject.Spec.Resource != nil {
		moduleCRAPIVersion, moduleCRKind := manifestObject.Spec.Resource.
			GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()
		moduleResource = &v1beta2.TrackingObject{
			PartialMeta: v1beta2.PartialMeta{
				Name:       manifestObject.Spec.Resource.GetName(),
				Namespace:  manifestObject.Spec.Resource.GetNamespace(),
				Generation: manifestObject.Spec.Resource.GetGeneration(),
			},
			TypeMeta: apimetav1.TypeMeta{Kind: moduleCRKind, APIVersion: moduleCRAPIVersion},
		}

		if module.Template.Annotations[shared.IsClusterScopedAnnotation] == shared.EnableLabelValue {
			moduleResource.PartialMeta.Namespace = ""
		}
	}

	moduleStatus := v1beta2.ModuleStatus{
		Name:    module.Name,
		FQDN:    module.FQDN,
		State:   manifestObject.Status.State,
		Channel: module.Template.DesiredChannel,
		Version: manifestObject.Spec.Version,
		Manifest: &v1beta2.TrackingObject{
			PartialMeta: v1beta2.PartialMeta{
				Name:       manifestObject.GetName(),
				Namespace:  manifestObject.GetNamespace(),
				Generation: manifestObject.GetGeneration(),
			},
			TypeMeta: apimetav1.TypeMeta{Kind: manifestKind, APIVersion: manifestAPIVersion},
		},
		Template: &v1beta2.TrackingObject{
			PartialMeta: v1beta2.PartialMeta{
				Name:       module.Template.GetName(),
				Namespace:  module.Template.GetNamespace(),
				Generation: module.Template.GetGeneration(),
			},
			TypeMeta: apimetav1.TypeMeta{Kind: templateKind, APIVersion: templateAPIVersion},
		},
		Resource: moduleResource,
	}

	if module.IsUnmanaged {
		moduleStatus.State = shared.StateUnmanaged
		moduleStatus.Manifest = nil
		moduleStatus.Template = nil
		moduleStatus.Resource = nil
	}

	return moduleStatus
}

func generateModuleStatusFromError(module *common.Module, existStatus *v1beta2.ModuleStatus) v1beta2.ModuleStatus {
	switch {
	case errors.Is(module.Template.Err, templatelookup.ErrTemplateUpdateNotAllowed):
		newModuleStatus := existStatus.DeepCopy()
		newModuleStatus.State = shared.StateWarning
		newModuleStatus.Message = module.Template.Err.Error()
		return *newModuleStatus
	case errors.Is(module.Template.Err, moduletemplateinfolookup.ErrNoTemplatesInListResult):
		return v1beta2.ModuleStatus{
			Name:    module.Name,
			Channel: module.Template.DesiredChannel,
			FQDN:    module.FQDN,
			State:   shared.StateWarning,
			Message: module.Template.Err.Error(),
		}
	case errors.Is(module.Template.Err, moduletemplateinfolookup.ErrWaitingForNextMaintenanceWindow):
		newModuleStatus := existStatus.DeepCopy()
		newModuleStatus.Message = module.Template.Err.Error()
		return *newModuleStatus
	case errors.Is(module.Template.Err, moduletemplateinfolookup.ErrFailedToDetermineIfMaintenanceWindowIsActive):
		newModuleStatus := existStatus.DeepCopy()
		newModuleStatus.Message = module.Template.Err.Error()
		newModuleStatus.State = shared.StateError
		return *newModuleStatus
	default:
		return v1beta2.ModuleStatus{
			Name:    module.Name,
			Channel: module.Template.DesiredChannel,
			FQDN:    module.FQDN,
			State:   shared.StateError,
			Message: module.Template.Err.Error(),
		}
	}
}
