package controllers

import (
	"flag"
	"fmt"
	operatorv1alpha1 "github.com/kyma-project/kyma-operator/operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"os"
	"path/filepath"
	"time"
)

func GetConfig() (*rest.Config, error) {
	// in-cluster config
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, err
	}

	// kubeconfig flag
	if flag.Lookup("kubeconfig") != nil {
		if kubeconfig := flag.Lookup("kubeconfig").Value.String(); kubeconfig != "" {
			return clientcmd.BuildConfigFromFlags("", kubeconfig)
		}
	}

	// env variable
	if len(os.Getenv("KUBECONFIG")) > 0 {
		return clientcmd.BuildConfigFromFlags("masterURL", os.Getenv("KUBECONFIG"))
	}

	// If no in-cluster config, try the default location in the user's home directory
	if home := homedir.HomeDir(); home != "" {
		return clientcmd.BuildConfigFromFlags("", filepath.Join(home, ".kube", "config"))
	}

	return nil, err
}

func areAllReadyConditionsSetForKyma(kymaObj *operatorv1alpha1.Kyma) bool {
	status := &kymaObj.Status
	if len(status.Conditions) < 1 {
		return false
	}
	for _, existingCondition := range status.Conditions {
		if existingCondition.Type == operatorv1alpha1.ConditionTypeReady &&
			existingCondition.Status != operatorv1alpha1.ConditionStatusTrue &&
			existingCondition.Reason != KymaKind {
			return false
		}
	}
	return true
}

func getReadyConditionForComponent(kymaObj *operatorv1alpha1.Kyma, componentName string) (*operatorv1alpha1.KymaCondition, bool) {
	status := &kymaObj.Status
	for _, existingCondition := range status.Conditions {
		if existingCondition.Type == operatorv1alpha1.ConditionTypeReady && existingCondition.Reason == componentName {
			return &existingCondition, true
		}
	}
	return &operatorv1alpha1.KymaCondition{}, false
}

func addReadyConditionForObjects(kymaObj *operatorv1alpha1.Kyma, componentNames []string, conditionStatus operatorv1alpha1.KymaConditionStatus, message string) {
	status := &kymaObj.Status
	for _, componentName := range componentNames {
		condition, exists := getReadyConditionForComponent(kymaObj, componentName)
		if !exists {
			condition = &operatorv1alpha1.KymaCondition{
				Type:   operatorv1alpha1.ConditionTypeReady,
				Reason: componentName,
			}
			status.Conditions = append(status.Conditions, *condition)
		}
		condition.LastTransitionTime = &metav1.Time{Time: time.Now()}
		condition.Message = message
		condition.Status = conditionStatus

		for i, existingCondition := range status.Conditions {
			if existingCondition.Type == operatorv1alpha1.ConditionTypeReady && existingCondition.Reason == componentName {
				status.Conditions[i] = *condition
				break
			}
		}
	}
}

func setComponentCRLabels(unstructuredCompCR *unstructured.Unstructured, componentName string, progression KymaProgressionInfo) {
	labels := unstructuredCompCR.Object["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
	labels["operator.kyma-project.io/controller-name"] = componentName
	labels["operator.kyma-project.io/applied-as"] = string(progression.KymaProgressionPath)
	labels["operator.kyma-project.io/release"] = progression.New
	unstructuredCompCR.Object["metadata"].(map[string]interface{})["labels"] = labels
}

func setObservedGeneration(kyma *operatorv1alpha1.Kyma) *operatorv1alpha1.Kyma {
	kyma.Status.ObservedGeneration = kyma.Generation
	return kyma
}

func setActiveRelease(kyma *operatorv1alpha1.Kyma) *operatorv1alpha1.Kyma {
	kyma.Status.ActiveRelease = kyma.Spec.Release
	return kyma
}

func getGvkAndSpecFromConfigMap(configMap *v1.ConfigMap, componentName string) (*schema.GroupVersionKind, interface{}, error) {
	componentBytes, ok := configMap.Data[componentName]
	if !ok {
		return nil, nil, fmt.Errorf("%s component not found for resource in ConfigMap", componentName)
	}
	componentYaml, err := getTemplatedComponent(componentBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("error during config map template parsing %w", err)
	}

	return &schema.GroupVersionKind{
		Group:   componentYaml["group"].(string),
		Kind:    componentYaml["kind"].(string),
		Version: componentYaml["version"].(string),
	}, componentYaml["spec"], nil
}

func getTemplatedComponent(componentTemplate string) (map[string]interface{}, error) {
	componentYaml := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(componentTemplate), &componentYaml); err != nil {
		return nil, fmt.Errorf("error during config map unmarshal %w", err)
	}
	return componentYaml, nil
}