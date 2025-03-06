package update

import (
	"context"

	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/pkg/module/common"
)

type UpdateStatusModulesExistingUC struct {
}

func NewUpdateStatusModulesExistingUC(manifestClient ManifestClient) *UpdateStatusModulesExistingUC {
	return &UpdateStatusModulesExistingUC{}
}

func (uc *UpdateStatusModulesExistingUC) Execute(ctx context.Context, kyma *v1beta2.Kyma, modules common.Modules) error {

	return nil
}
