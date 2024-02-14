package crd

import (
	"fmt"
	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
)

type Name string

func GetName(kind shared.Kind) Name {
	return Name(fmt.Sprintf("%s.%s", kind.Plural(), v1beta2.GroupVersion.Group))
}
