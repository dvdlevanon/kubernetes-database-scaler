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
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const DEPLOYMENT_ID_ANNOTATION_NAME = "kubernetes-database-scaler/deployment-id"
const ORIGINAL_OBSERVED_GENERATION_ANNOTATION_NAME = "kubernetes-database-scaler/original-observed-generation"
const VPA_ID_ANNOTATION_NAME = "kubernetes-database-scaler/vpa-id"

var logger = logging.MustGetLogger("controller")

type DeploymentReconciler struct {
	client.Client
	deploymentNamespace       string
	deploymentName            string
	deploymentColumnName      string
	vpaName                   string
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
	deploymentColumnName string, vpaName string, environments []string) (*DeploymentReconciler, error) {

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
		vpaName:                   vpaName,
		environmentsDefinitionMap: environmentsDefinitionMap,
	}, nil
}

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if req.Namespace != r.deploymentNamespace || req.Name != r.deploymentName {
		return ctrl.Result{}, nil
	}

	deployment := appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, &deployment)
	if err == nil {
		r.originalDeploymentChanged(ctx, deployment)
	} else if apierrors.IsNotFound(err) {
		r.originalDeploymentDeleted(ctx)
	} else {
		logger.Errorf("Unable to get deployment upon reconciling %s", err)
	}

	return ctrl.Result{}, nil
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
	if err := r.removeAllDeployments(ctx); err != nil {
		logger.Warningf("Unable to remove all deployments %s", err)
	}

	if err := r.removeAllVpas(ctx); err != nil {
		logger.Warningf("Unable to remove all vpas %s", err)
	}
}

func (r *DeploymentReconciler) removeAllDeployments(ctx context.Context) error {
	deployments, err := r.listDuplicatedDeployments(ctx)
	if err != nil {
		return err
	}

	logger.Infof("Original deployment deleted, removing %d duplicated deployments", len(deployments))

	for _, deployment := range deployments {
		if err := r.Delete(ctx, &deployment); err != nil {
			logger.Errorf("Error removing deployment %s", err)
			continue
		}
	}

	return nil
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

func (r *DeploymentReconciler) removeAllVpas(ctx context.Context) error {
	vpas, err := r.listDuplicatedVpas(ctx)
	if err != nil {
		return err
	}

	logger.Infof("Original deployment deleted, removing %d duplicated vpas", len(vpas))

	for _, vpa := range vpas {
		if err := r.Delete(ctx, &vpa); err != nil {
			logger.Errorf("Error removing vpa %s", err)
			continue
		}
	}

	return nil
}

func (r *DeploymentReconciler) listDuplicatedVpas(ctx context.Context) ([]vpa_types.VerticalPodAutoscaler, error) {
	vpas := vpa_types.VerticalPodAutoscalerList{}
	err := r.List(ctx, &vpas, client.InNamespace(r.deploymentNamespace))
	if err != nil {
		logger.Errorf("Error getting duplicated vpas %s", err)
		return nil, err
	}

	result := make([]vpa_types.VerticalPodAutoscaler, 0)
	for _, vpa := range vpas.Items {
		if _, ok := vpa.Annotations[VPA_ID_ANNOTATION_NAME]; ok {
			result = append(result, vpa)
		}
	}

	return result, nil
}

func (r *DeploymentReconciler) buildDeploymentName(deploymentSuffix string) string {
	return fmt.Sprintf("%s-%s", r.deploymentName, deploymentSuffix)
}

func (r *DeploymentReconciler) buildVpaName(vpaSuffix string) string {
	return fmt.Sprintf("%s-%s", r.vpaName, vpaSuffix)
}

func (r *DeploymentReconciler) getExistingVpa() (*vpa_types.VerticalPodAutoscaler, error) {
	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      r.vpaName,
	}

	vpa := vpa_types.VerticalPodAutoscaler{}
	if err := r.Get(context.Background(), key, &vpa); err != nil {
		logger.Errorf("Unable to get original vpa %v %s", key, err)
		return nil, err
	}

	return &vpa, nil
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

func (r *DeploymentReconciler) duplicateVpa(orig *vpa_types.VerticalPodAutoscaler, nameSuffix string) *vpa_types.VerticalPodAutoscaler {
	new := orig.DeepCopy()
	new.ObjectMeta = v1.ObjectMeta{
		Name:                       r.buildVpaName(nameSuffix),
		Namespace:                  orig.ObjectMeta.Namespace,
		Annotations:                orig.ObjectMeta.Annotations,
		Labels:                     orig.ObjectMeta.Labels,
		DeletionGracePeriodSeconds: orig.ObjectMeta.DeletionGracePeriodSeconds,
	}

	new.ObjectMeta.Annotations[VPA_ID_ANNOTATION_NAME] = nameSuffix
	new.Spec.TargetRef.Name = r.buildDeploymentName(nameSuffix)
	return new
}

func (r *DeploymentReconciler) duplicateDeployment(orig *appsv1.Deployment,
	nameSuffix string, environmentsMap map[string]string) *appsv1.Deployment {
	new := orig.DeepCopy()
	new.Status = appsv1.DeploymentStatus{}
	new.ObjectMeta = v1.ObjectMeta{
		Name:                       r.buildDeploymentName(nameSuffix),
		Namespace:                  orig.ObjectMeta.Namespace,
		Annotations:                orig.ObjectMeta.Annotations,
		Labels:                     orig.ObjectMeta.Labels,
		DeletionGracePeriodSeconds: orig.ObjectMeta.DeletionGracePeriodSeconds,
	}

	new.ObjectMeta.Annotations[ORIGINAL_OBSERVED_GENERATION_ANNOTATION_NAME] =
		fmt.Sprintf("%d", orig.Status.ObservedGeneration)
	new.ObjectMeta.Annotations[DEPLOYMENT_ID_ANNOTATION_NAME] = nameSuffix

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

func (r *DeploymentReconciler) createVpa(nameSuffix string) error {
	if r.vpaName == "" {
		return nil
	}

	logger.Infof("Creating a new vpa with suffix %v", nameSuffix)

	orig, err := r.getExistingVpa()
	if err != nil {
		return err
	}

	new := r.duplicateVpa(orig, nameSuffix)
	if err := r.Create(context.Background(), new); err != nil {
		logger.Errorf("Unable to create a new vpa for %s %s", nameSuffix, err)
		return err
	}

	return nil
}

func (r *DeploymentReconciler) isVpaExists(vpaSuffix string) (bool, error) {
	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      r.buildVpaName(vpaSuffix),
	}

	vpa := vpa_types.VerticalPodAutoscaler{}
	err := r.Get(context.Background(), key, &vpa)

	if err == nil {
		return true, nil
	}

	if apierrors.IsNotFound(err) {
		return false, nil
	}

	return false, err
}

func (r *DeploymentReconciler) isDeploymentExists(deploymentSuffix string) (bool, error) {
	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      r.buildDeploymentName(deploymentSuffix),
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

	vapExists, _ := r.isVpaExists(deploymentSuffix)
	if !vapExists {
		if err := r.createVpa(deploymentSuffix); err != nil {
			logger.Errorf("Unable to create VPA %s", err)
		}
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
