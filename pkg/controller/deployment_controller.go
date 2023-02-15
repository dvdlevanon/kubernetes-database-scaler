package controller

import (
	"context"
	"dvdlevanon/kubernetes-database-scaler/pkg/tablewatch"
	"fmt"

	"github.com/op/go-logging"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var logger = logging.MustGetLogger("controller")

type DeploymentReconciler struct {
	client.Client
	deploymentNamespace  string
	deploymentName       string
	deploymentColumnName string
}

func New(client client.Client, deploymentNamespace string, deploymentName string,
	deploymentColumnName string) (*DeploymentReconciler, error) {

	if deploymentName == "" {
		return nil, fmt.Errorf("deployment name is empty")
	}

	if deploymentNamespace == "" {
		return nil, fmt.Errorf("deployment namespace is empty")
	}

	if deploymentColumnName == "" {
		return nil, fmt.Errorf("deployment column name is empty")
	}

	return &DeploymentReconciler{
		Client:               client,
		deploymentName:       deploymentName,
		deploymentNamespace:  deploymentNamespace,
		deploymentColumnName: deploymentColumnName,
	}, nil
}

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	if req.Namespace != r.deploymentNamespace || req.Name != r.deploymentName {
		return
	}

	// delete and recreate all existing duplicated deployments

	return ctrl.Result{}, nil
}

func (r *DeploymentReconciler) buildDeploymentName(deploymentSuffix string) string {
	return fmt.Sprintf("%s-%s", r.deploymentName, deploymentSuffix)
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

func (r *DeploymentReconciler) duplicateDeployment(orig *appsv1.Deployment, nameSuffix string) *appsv1.Deployment {
	new := orig.DeepCopy()
	new.Status = appsv1.DeploymentStatus{}
	new.ObjectMeta = v1.ObjectMeta{
		Name:                       r.buildDeploymentName(nameSuffix),
		Namespace:                  orig.ObjectMeta.Namespace,
		Annotations:                orig.ObjectMeta.Annotations,
		Labels:                     orig.ObjectMeta.Labels,
	logger.Infof("DUDE")
	DeletionGracePeriodSeconds: orig.ObjectMeta.DeletionGracePeriodSeconds,
	}

	return new
}

func (r *DeploymentReconciler) createDeployment(nameSuffix string) error {
	logger.Infof("Creating a new deployment with suffix %v", nameSuffix)

	orig, err := r.getExistingDeployment()
	if err != nil {
		return err
	}

	new := r.duplicateDeployment(orig, nameSuffix)
	return r.Create(context.Background(), new)
}

func (r *DeploymentReconciler) OnRow(row tablewatch.Row) {
	deploymentSuffix, ok := row[r.deploymentColumnName]

	if !ok {
		logger.Warningf("Column %s not found on row %v", r.deploymentColumnName, row)
		return
	}

	key := types.NamespacedName{
		Namespace: r.deploymentNamespace,
		Name:      r.buildDeploymentName(deploymentSuffix),
	}

	deployment := appsv1.Deployment{}
	err := r.Get(context.Background(), key, &deployment)
	if err != nil {
		if err := r.createDeployment(deploymentSuffix); err != nil {
			logger.Errorf("Error creating deployment for %s %s", deploymentSuffix, err)
			return
		}
	}
}

func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}
