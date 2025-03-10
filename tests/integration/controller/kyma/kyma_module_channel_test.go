package kyma_test

import (
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"ocm.software/ocm/api/ocm/compdesc"
	compdescv2 "ocm.software/ocm/api/ocm/compdesc/versions/v2"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
	"github.com/kyma-project/lifecycle-manager/pkg/testutils/builder"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/kyma-project/lifecycle-manager/pkg/testutils"
)

const (
	ValidChannel            = "valid"
	InValidChannel          = "Invalid01"                                       // lower case characters from a to z
	InValidMinLengthChannel = "ch"                                              // minlength = 3
	InValidMaxLengthChannel = "averylongchannelwhichlargerthanallowedmaxlength" // maxlength = 32
	LowerVersion            = "0.0.1"
	HigherVersion           = "0.0.2"
)

var errTemplateInfoChannelMismatch = errors.New("mismatch in template info channel")

var _ = Describe("valid kyma.spec.channel should be deployed successful", func() {
	kyma := NewTestKyma("kyma")
	It("should create kyma with standard modules in a valid channel", func() {
		kyma.Spec.Channel = ValidChannel
		Expect(kcpClient.Create(ctx, kyma)).ToNot(HaveOccurred())
	})
	DescribeTable(
		"Test Channel Status", func(givenCondition func() error, expectedBehavior func() error) {
			Eventually(givenCondition, Timeout, Interval).Should(Succeed())
			Eventually(expectedBehavior, Timeout, Interval).Should(Succeed())
		},
		Entry(
			"When kyma is deployed in valid channel,"+
				" expect Modules to be in valid channel",
			givenModuleTemplateWithChannel(ValidChannel, true),
			expectEveryModuleStatusToHaveChannel(kyma.Name, ValidChannel),
		),
	)
})

var _ = Describe("module channel different from the global channel", func() {
	kyma := NewTestKyma("kyma")
	moduleName := "test-different-channel"

	kyma.Spec.Modules = append(
		kyma.Spec.Modules, v1beta2.Module{
			ControllerName: "manifest",
			Name:           moduleName,
			Channel:        FastChannel,
			Managed:        true,
		})
	It("should create kyma with standard modules in a valid channel", func() {
		kyma.Spec.Channel = ValidChannel
		Expect(kcpClient.Create(ctx, kyma)).ToNot(HaveOccurred())
	})

	It("Should deploy ModuleTemplate in fast channel", func() {
		Eventually(deployModuleInChannel).WithArguments(FastChannel, moduleName).Should(Succeed())
	})

	It("Manifest should be deployed in fast channel", func() {
		Eventually(expectModuleManifestToHaveChannel, Timeout, Interval).WithArguments(
			kyma.GetName(), kyma.GetNamespace(), moduleName, FastChannel).Should(Succeed())
	})
})

var _ = Describe("Given invalid channel which is rejected by CRD validation rules", func() {
	DescribeTable(
		"Test kyma CR, module template creation", func(givenCondition func() error) {
			Eventually(givenCondition, Timeout, Interval).Should(Succeed())
		},
		Entry(
			"invalid channel with not allowed characters",
			givenModuleTemplateWithChannel(InValidChannel, false),
		),
		Entry(
			"invalid channel with less than min length",
			givenModuleTemplateWithChannel(InValidMinLengthChannel, false),
		),
		Entry(
			"invalid channel with more than max length",
			givenModuleTemplateWithChannel(InValidMaxLengthChannel, false),
		),
		Entry(
			"invalid channel with not allowed characters",
			givenKymaWithInvalidChannel(InValidChannel),
		),
		Entry(
			"invalid channel with less than min length",
			givenKymaWithInvalidChannel(InValidMinLengthChannel),
		),
		Entry(
			"invalid channel with more than max length",
			givenKymaWithInvalidChannel(InValidMaxLengthChannel),
		),
		Entry(
			"invalid channel with not allowed characters",
			givenKymaSpecModulesWithInvalidChannel(InValidChannel),
		),
		Entry(
			"invalid channel with less than min length",
			givenKymaSpecModulesWithInvalidChannel(InValidMinLengthChannel),
		),
		Entry(
			"invalid channel with more than max length",
			givenKymaSpecModulesWithInvalidChannel(InValidMaxLengthChannel),
		),
	)
})

func givenModuleTemplateWithChannel(channel string, isValid bool) func() error {
	return func() error {
		modules := []v1beta2.Module{
			{
				ControllerName: "manifest",
				Name:           "module-with-" + channel,
				Channel:        channel,
				Managed:        true,
			},
		}
		err := createModuleTemplateSetsForKyma(modules, LowerVersion, channel)
		if isValid {
			return err
		}
		return ignoreInvalidError(err)
	}
}

func deployModuleInChannel(channel string, moduleName string) error {
	modules := []v1beta2.Module{
		{
			ControllerName: "manifest",
			Name:           moduleName,
			Channel:        channel,
			Managed:        true,
		},
	}
	err := createModuleTemplateSetsForKyma(modules, LowerVersion, channel)
	return err
}

func givenKymaWithInvalidChannel(channel string) func() error {
	return func() error {
		kyma := NewTestKyma("kyma")
		kyma.Spec.Channel = channel
		err := kcpClient.Create(ctx, kyma)
		return ignoreInvalidError(err)
	}
}

func ignoreInvalidError(err error) error {
	var statusError *apierrors.StatusError
	ok := errors.As(err, &statusError)
	Expect(ok).Should(BeTrue())
	if statusError.ErrStatus.Reason != apimetav1.StatusReasonInvalid {
		return fmt.Errorf("status error not match: expect %s, actual %w", apimetav1.StatusReasonInvalid, err)
	}
	return nil
}

func givenKymaSpecModulesWithInvalidChannel(channel string) func() error {
	return func() error {
		kyma := NewTestKyma("kyma")
		kyma.Spec.Modules = append(
			kyma.Spec.Modules, v1beta2.Module{
				ControllerName: "manifest",
				Name:           "module-with-" + channel,
				Channel:        channel,
				Managed:        true,
			})
		err := kcpClient.Create(ctx, kyma)
		return ignoreInvalidError(err)
	}
}

var _ = Describe("Channel switch", Ordered, func() {
	kyma := NewTestKyma("empty-module-kyma")

	kyma.Spec.Modules = append(
		kyma.Spec.Modules, v1beta2.Module{
			ControllerName: "manifest",
			Name:           "channel-switch",
			Channel:        v1beta2.DefaultChannel,
			Managed:        true,
		})

	AfterAll(func() {
		Eventually(DeleteCR, Timeout, Interval).
			WithContext(ctx).
			WithArguments(kcpClient, kyma).Should(Succeed())
	})

	BeforeAll(func() {
		Expect(createModuleTemplateSetsForKyma(kyma.Spec.Modules, LowerVersion, v1beta2.DefaultChannel)).To(Succeed())
		Expect(createModuleTemplateSetsForKyma(kyma.Spec.Modules, HigherVersion, FastChannel)).To(Succeed())
	})

	AfterAll(CleanupModuleTemplateSetsForKyma(kyma))

	It(
		"should create kyma with standard modules in default channel normally", func() {
			Eventually(CreateCR, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kcpClient, kyma).Should(Succeed())
			Eventually(KymaIsInState, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kyma.GetName(), kyma.GetNamespace(), kcpClient, shared.StateProcessing).
				Should(Succeed())
			for _, module := range kyma.Spec.Modules {
				Eventually(UpdateManifestState, Timeout, Interval).
					WithArguments(ctx, kcpClient,
						kyma.GetName(), kyma.GetNamespace(), module.Name, shared.StateReady).Should(Succeed())
			}
			Eventually(KymaIsInState, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kyma.GetName(), kyma.GetNamespace(), kcpClient, shared.StateReady).
				Should(Succeed())
		},
	)

	DescribeTable(
		"Test Channel Status", func(givenCondition func() error, expectedBehavior func() error) {
			Eventually(givenCondition, Timeout, Interval).Should(Succeed())
			Eventually(expectedBehavior, Timeout, Interval).Should(Succeed())
		},
		Entry(
			"When kyma is deployed in default channel with lower version,"+
				" expect Modules to be in regular channel",
			noCondition(),
			expectEveryModuleStatusToHaveChannel(kyma.Name, v1beta2.DefaultChannel),
		),
		Entry(
			"When all modules are updated to fast channel with higher version,"+
				" expect Modules to update to fast channel",
			whenUpdatingEveryModuleChannel(kyma.GetName(), kyma.GetNamespace(), FastChannel),
			expectEveryModuleStatusToHaveChannel(kyma.Name, FastChannel),
		),
	)

	It("When all modules are reverted to regular channel,"+
		" expect Modules to stay in fast channel", func() {
		Eventually(UpdateKymaModuleChannel, Timeout, Interval).
			WithContext(ctx).
			WithArguments(kcpClient, kyma.GetName(), kyma.GetNamespace(), v1beta2.DefaultChannel).
			Should(Succeed())
		Consistently(expectEveryModuleStatusToHaveChannel(kyma.Name, FastChannel), ConsistentCheckTimeout, Interval).
			Should(Succeed())
		Consistently(expectEveryManifestToHaveChannel, ConsistentCheckTimeout, Interval).
			WithArguments(kyma.GetName(), kyma.GetNamespace(), FastChannel).
			Should(Succeed())
	})

	It(
		"should lead to kyma being warning in the end of the channel switch", func() {
			Eventually(KymaIsInState, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kyma.GetName(), kyma.GetNamespace(), kcpClient, shared.StateWarning).
				Should(Succeed())
		},
	)
},
)

func CleanupModuleTemplateSetsForKyma(kyma *v1beta2.Kyma) func() {
	return func() {
		By("Cleaning up decremented ModuleTemplate set in regular")
		for _, module := range kyma.Spec.Modules {
			template := builder.NewModuleTemplateBuilder().
				WithNamespace(ControlPlaneNamespace).
				WithName(fmt.Sprintf("%s-%s", module.Name, v1beta2.DefaultChannel)).
				WithLabelModuleName(module.Name).
				WithChannel(module.Channel).
				WithOCM(compdescv2.SchemaVersion).Build()
			Eventually(DeleteCR, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kcpClient, template).Should(Succeed())
		}
		By("Cleaning up standard ModuleTemplate set in fast")
		for _, module := range kyma.Spec.Modules {
			template := builder.NewModuleTemplateBuilder().
				WithNamespace(ControlPlaneNamespace).
				WithName(fmt.Sprintf("%s-%s", module.Name, FastChannel)).
				WithLabelModuleName(module.Name).
				WithChannel(module.Channel).
				WithOCM(compdescv2.SchemaVersion).Build()
			Eventually(DeleteCR, Timeout, Interval).
				WithContext(ctx).
				WithArguments(kcpClient, template).Should(Succeed())
		}
	}
}

func expectEveryModuleStatusToHaveChannel(kymaName, channel string) func() error {
	return func() error {
		return templateInfosMatchChannel(kymaName, channel)
	}
}

func templateInfosMatchChannel(kymaName, channel string) error {
	kyma, err := GetKyma(ctx, kcpClient, kymaName, ControlPlaneNamespace)
	if err != nil {
		return err
	}
	for i := range kyma.Status.Modules {
		if kyma.Status.Modules[i].Channel != channel {
			return fmt.Errorf(
				"%w: %s should be %s",
				errTemplateInfoChannelMismatch, kyma.Status.Modules[i].Channel, channel,
			)
		}
	}
	return nil
}

func expectEveryManifestToHaveChannel(kymaName, kymaNamespace, channel string) error {
	kyma, err := GetKyma(ctx, kcpClient, kymaName, kymaNamespace)
	if err != nil {
		return err
	}
	for _, module := range kyma.Spec.Modules {
		manifest, err := GetManifest(ctx, kcpClient, kyma.GetName(), kyma.GetNamespace(), module.Name)
		if err != nil {
			return err
		}
		manifestChannel, found := manifest.GetChannel()
		if found {
			if manifestChannel != channel {
				return fmt.Errorf(
					"%w: %s should be %s",
					errTemplateInfoChannelMismatch, manifestChannel, channel,
				)
			}
			return nil
		}
	}
	return fmt.Errorf(
		"%w: no %s found",
		errTemplateInfoChannelMismatch, channel,
	)
}

func expectModuleManifestToHaveChannel(kymaName, kymaNamespace, moduleName, channel string) error {
	kyma, err := GetKyma(ctx, kcpClient, kymaName, kymaNamespace)
	if err != nil {
		return err
	}

	manifest, err := GetManifest(ctx, kcpClient, kyma.GetName(), kyma.GetNamespace(), moduleName)
	if err != nil {
		return err
	}
	manifestChannel, found := manifest.GetChannel()
	if found {
		if manifestChannel != channel {
			return fmt.Errorf(
				"%w: %s should be %s",
				errTemplateInfoChannelMismatch, manifestChannel, channel,
			)
		}
		return nil
	}
	return fmt.Errorf(
		"%w: no %s found",
		errTemplateInfoChannelMismatch, channel,
	)
}

func whenUpdatingEveryModuleChannel(kymaName, kymaNamespace, channel string) func() error {
	return func() error {
		return UpdateKymaModuleChannel(ctx, kcpClient, kymaName, kymaNamespace, channel)
	}
}

func createModuleTemplateSetsForKyma(modules []v1beta2.Module, modifiedVersion, channel string) error {
	for _, module := range modules {
		template := builder.NewModuleTemplateBuilder().
			WithNamespace(ControlPlaneNamespace).
			WithLabelModuleName(module.Name).
			WithChannel(module.Channel).
			WithOCM(compdescv2.SchemaVersion).Build()

		descriptor, err := descriptorProvider.GetDescriptor(template)
		if err != nil {
			return err
		}
		descriptor.Version = modifiedVersion
		newDescriptor, err := compdesc.Encode(descriptor.ComponentDescriptor, compdesc.DefaultJSONCodec)
		if err != nil {
			return err
		}
		template.Spec.Descriptor.Raw = newDescriptor
		template.Spec.Channel = channel
		template.Name = fmt.Sprintf("%s-%s", template.Name, channel)
		if err := kcpClient.Create(ctx, template); err != nil {
			return err
		}
	}
	return nil
}
