package controller

import (
	"context"
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"fmt"
	"strings"

	"github.com/op/go-logging"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const DEPLOYMENT_ID_ANNOTATION_NAME = "kubernetes-database-scaler/deployment-id"
const ORIGINAL_OBSERVED_GENERATION_ANNOTATION_NAME = "kubernetes-database-scaler/original-observed-generation"

var logger = logging.MustGetLogger("controller")

type DeploymentReconciler struct {
	client.Client
	deploymentNamespace       string
	deploymentName            string
	deploymentColumnName      string
	environmentsDefinitionMap map[string]string
}

func buildEnvironmentDefinitionMap(environments []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, environment := range environments {
		parts := strings.Split(environment, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid environment format %s (e.g name=column_name)", environment)
		}

		result[parts[0]] = parts[1]
	}

	return result, nil
}

func New(client client.Client, deploymentNamespace string, deploymentName string,
	deploymentColumnName string, environments []string) (*DeploymentReconciler, error) {

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name is empty")
	}

	if deploymentNamespace == "" {
		return nil, fmt.Errorf("deployment namespace is empty")
	}

	if deploymentColumnName == "" {
		return nil, fmt.Errorf("deployment column name is empty")
	}

	environmentsDefinitionMap, err := buildEnvironmentDefinitionMap(environments)
	if err != nil {
		return nil, err
	}

	return &DeploymentReconciler{
		Client:                    client,
		deploymentName:            deploymentName,
		deploymentNamespace:       deploymentNamespace,
		deploymentColumnName:      deploymentColumnName,
		environmentsDefinitionMap: environmentsDefinitionMap,
	}, nil
}

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if req.Namespace == r.deploymentNamespace && req.Name == r.deploymentName {
		r.reconcileDeployment(ctx, req)
	}

	return ctrl.Result{}, nil
}

func (r *DeploymentReconciler) reconcileDeployment(ctx context.Context, req ctrl.Request) {
	deployment := appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, &deployment)
	if err == nil {
		r.originalDeploymentChanged(ctx, deployment)
	} else if apierrors.IsNotFound(err) {
		r.originalDeploymentDeleted(ctx)
	} else {
		logger.Errorf("Unable to get deployment upon reconciling %s", err)
	}
}

func (r *DeploymentReconciler) originalDeploymentChanged(ctx context.Context, original appsv1.Deployment) error {
	deployments, err := r.listDuplicatedDeployments(ctx)
	if err != nil {
		return err
	}

	actualObservedGeneration := fmt.Sprintf("%d", original.Status.ObservedGeneration)
	logger.Infof("Original deployment may changed (generation %s), updating %d duplicated deployments",
		actualObservedGeneration, len(deployments))
	for _, deployment := range deployments {
		origObserevedGeneration, ok := deployment.Annotations[ORIGINAL_OBSERVED_GENERATION_ANNOTATION_NAME]
		if !ok {
			logger.Errorf("Error getting original observed generation annotation from %v", deployment)
			continue
		}

		if actualObservedGeneration == origObserevedGeneration {
			continue
		}

		if err := r.Delete(ctx, &deployment); err != nil {
			logger.Errorf("Unable to remove deployment %s", err)
			continue
		}

		nameSuffix, ok := deployment.Annotations[DEPLOYMENT_ID_ANNOTATION_NAME]
		if !ok {
			logger.Errorf("Unable to get name suffix annotation from %v", deployment)
			continue
		}

		environmentMap, err := r.buildEnvironmentMapFromDeployment(deployment)
		if err != nil {
			logger.Errorf("Unable to build envrionment map from deployment %s", err)
			continue
		}

		r.createDeployment(nameSuffix, environmentMap)
	}

	return nil
}

func (r *DeploymentReconciler) originalDeploymentDeleted(ctx context.Context) {
	deployments, err := r.listDuplicatedDeployments(ctx)
	if err != nil {
		return
	}

	logger.Infof("Original deployment deleted, removing %d duplicated deployments", len(deployments))

	for _, deployment := range deployments {
		if err := r.Delete(ctx, &deployment); err != nil {
			logger.Errorf("Error removing deployment %s", err)
			continue
		}
	}
}

func (r *DeploymentReconciler) listDuplicatedDeployments(ctx context.Context) ([]appsv1.Deployment, error) {
	deployments := appsv1.DeploymentList{}
	err := r.List(ctx, &deployments, client.InNamespace(r.deploymentNamespace))
	if err != nil {
		logger.Errorf("Error getting duplicated deployments %s", err)
		return nil, err
	}

	result := make([]appsv1.Deployment, 0)
	for _, deployment := range deployments.Items {
		if _, ok := deployment.Annotations[DEPLOYMENT_ID_ANNOTATION_NAME]; ok {
			result = append(result, deployment)
		}
	}

	return result, nil
}

func buildDeploymentName(deploymentName string, deploymentSuffix string) string {
	return fmt.Sprintf("%s-%s", deploymentName, deploymentSuffix)
}

func (r *DeploymentReconciler) getExistingDeployment() (*appsv1.Deployment, error) {
	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      r.deploymentName,
	}

	deployment := appsv1.Deployment{}
	if err := r.Get(context.Background(), key, &deployment); err != nil {
		logger.Errorf("Unable to get original deployment %v %s", key, err)
		return nil, err
	}

	return &deployment, nil
}

func (r *DeploymentReconciler) replaceOrAddEnv(envs []corev1.EnvVar, name string, value string) []corev1.EnvVar {
	newEnvs := make([]corev1.EnvVar, 0)
	for _, env := range envs {
		if env.Name == name {
			continue
		}

		newEnvs = append(newEnvs, env)
	}

	newEnvs = append(newEnvs, corev1.EnvVar{
		Name:  name,
		Value: value,
	})

	return newEnvs
}

func (r *DeploymentReconciler) duplicateDeployment(orig *appsv1.Deployment,
	nameSuffix string, environmentsMap map[string]string) *appsv1.Deployment {
	new := orig.DeepCopy()
	new.Status = appsv1.DeploymentStatus{}
	new.ObjectMeta = v1.ObjectMeta{
		Name:                       buildDeploymentName(r.deploymentName, nameSuffix),
		Namespace:                  orig.ObjectMeta.Namespace,
		Annotations:                orig.ObjectMeta.Annotations,
		Labels:                     orig.ObjectMeta.Labels,
		DeletionGracePeriodSeconds: orig.ObjectMeta.DeletionGracePeriodSeconds,
	}

	new.ObjectMeta.Annotations[ORIGINAL_OBSERVED_GENERATION_ANNOTATION_NAME] =
		fmt.Sprintf("%d", orig.Status.ObservedGeneration)
	new.ObjectMeta.Annotations[DEPLOYMENT_ID_ANNOTATION_NAME] = nameSuffix

	for key, value := range new.Spec.Selector.MatchLabels {
		if key == "name" && value == orig.ObjectMeta.Name {
			new.Spec.Selector.MatchLabels[value] = r.buildDeploymentName(nameSuffix)
		}
	}

	for key, value := range new.Spec.Template.ObjectMeta.Labels {
		if key == "name" && value == orig.ObjectMeta.Name {
			new.Spec.Template.ObjectMeta.Labels[value] = r.buildDeploymentName(nameSuffix)
		}
	}

	for i := range new.Spec.Template.Spec.Containers {
		for name, value := range environmentsMap {
			new.Spec.Template.Spec.Containers[i].Env = r.replaceOrAddEnv(new.Spec.Template.Spec.Containers[i].Env, name, value)
		}
	}

	return new
}

func (r *DeploymentReconciler) createDeployment(nameSuffix string, environmentsMap map[string]string) error {
	logger.Infof("Creating a new deployment with suffix %v", nameSuffix)
	orig, err := r.getExistingDeployment()
	if err != nil {
		return err
	}

	new := r.duplicateDeployment(orig, nameSuffix, environmentsMap)
	if err := r.Create(context.Background(), new); err != nil {
		logger.Errorf("Unable to create a new deployment for %s %s", nameSuffix, err)
		return err
	}

	return nil
}

func (r *DeploymentReconciler) isDeploymentExists(deploymentSuffix string) (bool, error) {
	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      buildDeploymentName(r.deploymentName, deploymentSuffix),
	}

	deployment := appsv1.Deployment{}
	err := r.Get(context.Background(), key, &deployment)

	if err == nil {
		return true, nil
	}

	if apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

func (r *DeploymentReconciler) OnRow(row tablewatch.Row) {
	deploymentSuffix, ok := row[r.deploymentColumnName]
	if !ok {
		logger.Warningf("Column %s not found on row %v", r.deploymentColumnName, row)
		return
	}

	exists, err := r.isDeploymentExists(deploymentSuffix)
	if exists {
		return
	}

	if err != nil {
		logger.Errorf("Unable to get deployment info for %s %s", deploymentSuffix, err)
		return
	}

	environmentsMap, err := r.buildEnvironmentMapFromRow(row)
	if err != nil {
		logger.Errorf("Unable to build environment map %s", err)
		return
	}

	r.createDeployment(deploymentSuffix, environmentsMap)
}

func (r *DeploymentReconciler) buildEnvironmentMapFromRow(row tablewatch.Row) (map[string]string, error) {
	result := make(map[string]string, 0)

	for name, columnName := range r.environmentsDefinitionMap {
		val, ok := row[columnName]
		if !ok {
			return nil, fmt.Errorf("value of column %s not found in row %v", columnName, row)
		}

		result[name] = val
	}

	return result, nil
}

func (r *DeploymentReconciler) getEnvValue(envs []corev1.EnvVar, name string) (string, bool) {
	for _, env := range envs {
		if env.Name == name {
			return env.Value, true
		}
	}

	return "", false
}

func (r *DeploymentReconciler) buildEnvironmentMapFromDeployment(deployment appsv1.Deployment) (map[string]string, error) {
	result := make(map[string]string, 0)

	allEnvs := make([]corev1.EnvVar, 0)
	for _, container := range deployment.Spec.Template.Spec.Containers {
		allEnvs = append(allEnvs, container.Env...)
	}

	for name, columnName := range r.environmentsDefinitionMap {
		val, found := r.getEnvValue(allEnvs, name)
		if !found {
			return nil, fmt.Errorf("value of column %s not found in deployment", columnName)
		}

		result[name] = val
	}

	return result, nil
}

func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}
