package controller

import (
	"context"
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const VPA_ID_ANNOTATION_NAME = "kubernetes-database-scaler/vpa-id"

type VpaReconciler struct {
	client.Client
	vpaNamespace   string
	vpaName        string
	vpaColumnName  string
	deploymentName string
}

func NewVpaController(client client.Client, vpaNamespace string,
	vpaName string, vpaColumnName string, deploymentName string) (*VpaReconciler, error) {

	if vpaNamespace == "" {
		return nil, fmt.Errorf("vpa name is empty")
	}

	if vpaNamespace == "" {
		return nil, fmt.Errorf("vpa` namespace is empty")
	}

	if vpaColumnName == "" {
		return nil, fmt.Errorf("vpa column name is empty")
	}

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name is empty")
	}

	return &VpaReconciler{
		Client:         client,
		vpaName:        vpaName,
		vpaNamespace:   vpaNamespace,
		vpaColumnName:  vpaColumnName,
		deploymentName: deploymentName,
	}, nil
}

func (r *VpaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if req.Namespace == r.vpaNamespace && req.Name == r.vpaName {
		r.reconcileVpa(ctx, req)
	}

	return ctrl.Result{}, nil
}

func (r *VpaReconciler) reconcileVpa(ctx context.Context, req ctrl.Request) {
	vpa := vpa_types.VerticalPodAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, &vpa)
	if err == nil {
		r.originalVpaChanged(ctx, vpa)
	} else if apierrors.IsNotFound(err) {
		r.originalVpaDeleted(ctx)
	} else {
		logger.Errorf("Unable to get vpa upon reconciling %s", err)
	}
}

func (r *VpaReconciler) originalVpaChanged(ctx context.Context, original vpa_types.VerticalPodAutoscaler) error {
	vpas, err := r.listDuplicatedVpas(ctx)
	if err != nil {
		return err
	}

	for _, vpa := range vpas {
		if err := r.Delete(ctx, &vpa); err != nil {
			logger.Errorf("Unable to remove vpa %s", err)
			continue
		}

		nameSuffix, ok := vpa.Annotations[VPA_ID_ANNOTATION_NAME]
		if !ok {
			logger.Errorf("Unable to get name suffix annotation from vpa %v", vpa)
			continue
		}

		r.createVpa(nameSuffix)
	}

	return nil
}

func (r *VpaReconciler) originalVpaDeleted(ctx context.Context) {
	vpas, err := r.listDuplicatedVpas(ctx)
	if err != nil {
		return
	}

	logger.Infof("Original vpa deleted, removing %d duplicated vpas", len(vpas))

	for _, vpa := range vpas {
		if err := r.Delete(ctx, &vpa); err != nil {
			logger.Errorf("Error removing vpa %s", err)
			continue
		}
	}
}

func (r *VpaReconciler) listDuplicatedVpas(ctx context.Context) ([]vpa_types.VerticalPodAutoscaler, error) {
	vpas := vpa_types.VerticalPodAutoscalerList{}
	err := r.List(ctx, &vpas, client.InNamespace(r.vpaNamespace))
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

func (r *VpaReconciler) buildVpaName(vpaSuffix string) string {
	return fmt.Sprintf("%s-%s", r.vpaName, vpaSuffix)
}

func (r *VpaReconciler) getExistingVpa() (*vpa_types.VerticalPodAutoscaler, error) {
	key := types.NamespacedName{
		Namespace: r.vpaNamespace,
		Name:      r.vpaName,
	}

	vpa := vpa_types.VerticalPodAutoscaler{}
	if err := r.Get(context.Background(), key, &vpa); err != nil {
		logger.Errorf("Unable to get original vpa %v %s", key, err)
		return nil, err
	}

	return &vpa, nil
}

func (r *VpaReconciler) duplicateVpa(orig *vpa_types.VerticalPodAutoscaler, nameSuffix string) *vpa_types.VerticalPodAutoscaler {
	new := orig.DeepCopy()
	new.ObjectMeta = v1.ObjectMeta{
		Name:                       r.buildVpaName(nameSuffix),
		Namespace:                  orig.ObjectMeta.Namespace,
		Annotations:                orig.ObjectMeta.Annotations,
		Labels:                     orig.ObjectMeta.Labels,
		DeletionGracePeriodSeconds: orig.ObjectMeta.DeletionGracePeriodSeconds,
	}

	new.ObjectMeta.Annotations[VPA_ID_ANNOTATION_NAME] = nameSuffix
	new.Spec.TargetRef.Name = buildDeploymentName(r.deploymentName, nameSuffix)
	return new
}

func (r *VpaReconciler) createVpa(nameSuffix string) error {
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

func (r *VpaReconciler) isVpaExists(vpaSuffix string) (bool, error) {
	key := types.NamespacedName{
		Namespace: r.vpaNamespace,
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

func (r *VpaReconciler) OnRow(row tablewatch.Row) {
	deploymentSuffix, ok := row[r.vpaColumnName]
	if !ok {
		logger.Warningf("Column %s not found on row %v", r.vpaColumnName, row)
		return
	}

	exists, err := r.isVpaExists(deploymentSuffix)
	if exists {
		return
	}

	if err != nil {
		logger.Errorf("Unable to get VPA info for %s %s", deploymentSuffix, err)
		return
	}

	r.createVpa(deploymentSuffix)
}

func (r *VpaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vpa_types.VerticalPodAutoscaler{}).
		Complete(r)
}
