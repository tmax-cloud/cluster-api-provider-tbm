/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "cluster-api-provider-tbm/api/v1alpha3"

	infraUtil "cluster-api-provider-tbm/controllers/util"

	"sigs.k8s.io/controller-runtime/pkg/handler"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TbmClusterReconciler reconciles a TbmCluster object
type TbmClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmpools,verbs=get;list;watch;create;update;patch;

var log logr.Logger

func (r *TbmClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.TODO()
	log = r.Log.WithValues("namespace", req.Namespace, "tbmCluster", req.Name)

	// Fetch the TbmCluster instance
	tbmCluster := &infrav1.TbmCluster{}
	err := r.Get(ctx, req.NamespacedName, tbmCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, tbmCluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}

	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	if util.IsPaused(cluster, tbmCluster) {
		log.Info("TbmCluster or linked Cluster is marked as paused. Won't reconcile")
		return reconcile.Result{}, nil
	}

	// Handle deleted clusters
	if !tbmCluster.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(tbmCluster)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(tbmCluster)
}

func (r *TbmClusterReconciler) reconcileDelete(tbmCluster *infrav1.TbmCluster) (reconcile.Result, error) {
	oldTbmCluster := tbmCluster

	// Cluster is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(oldTbmCluster, infrav1.ClusterFinalizer)

	return reconcile.Result{}, nil
}

func (r *TbmClusterReconciler) reconcileNormal(oldTbmCluster *infrav1.TbmCluster) (reconcile.Result, error) {
	newTbmCluster := oldTbmCluster.DeepCopy()
	if &newTbmCluster.Status == nil {
		newTbmCluster.Status = infrav1.TbmClusterStatus{}
	}

	if newTbmCluster.Status.Ready {
		log.Info("apiEndpoint already exists")
		return reconcile.Result{}, nil
	}

	// If the TbmCluster doesn't have our finalizer, add it.
	//	controllerutil.AddFinalizer(oldTbmCluster, infrav1.ClusterFinalizer)

	//logic here to prepare cluster
	apiEndpoint := r.getAPIEndpointfromTbmPool(oldTbmCluster)
	if apiEndpoint == "" {
		log.Info("there is no proper tmb in tbmPool. Wait until proper tbm is appeared")
		return reconcile.Result{}, nil
	}
	newTbmCluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{
		Host: apiEndpoint,
		Port: 6443,
	}

	//patch tbmCluster
	newTbmCluster.Status.Ready = true
	if err := r.Status().Patch(context.TODO(), newTbmCluster, client.MergeFrom(oldTbmCluster)); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.Patch(context.TODO(), newTbmCluster, client.MergeFrom(oldTbmCluster)); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *TbmClusterReconciler) getAPIEndpointfromTbmPool(tbmCluster *infrav1.TbmCluster) string {
	tbmPoolList := &infrav1.TbmPoolList{}

	r.List(context.TODO(), tbmPoolList)
	listOptions := []client.ListOption{
		client.MatchingLabels(map[string]string{infraUtil.LabelValid: "true"}),
	}
	r.List(context.TODO(), tbmPoolList, listOptions...)

	if &tbmPoolList.Items[0] == nil {
		return ""
	}

	oldTbmPool := &tbmPoolList.Items[0]
	newTbmPool := oldTbmPool.DeepCopy()

	newTbmPool.Labels[infraUtil.LabelClusterRole] = infraUtil.LabelClusterRoleMaster
	newTbmPool.Labels[infraUtil.LabelClusterName] = tbmCluster.Name + infraUtil.LabelClusterNameProvisioning

	r.Patch(context.TODO(), newTbmPool, client.MergeFrom(oldTbmPool))

	return newTbmPool.Spec.SSH.IP
}

func (r *TbmClusterReconciler) requeueTbmClusterForUnpausedCluster(o handler.MapObject) []ctrl.Request {
	c := o.Object.(*clusterv1.Cluster)
	log := r.Log.WithValues("objectMapper", "clusterToTbmCluster", "namespace", c.Namespace, "cluster", c.Name)

	// Don't handle deleted clusters
	if !c.ObjectMeta.DeletionTimestamp.IsZero() {
		log.V(4).Info("Cluster has a deletion timestamp, skipping mapping.")
		return nil
	}

	// Make sure the ref is set
	if c.Spec.InfrastructureRef == nil {
		log.V(4).Info("Cluster does not have an InfrastructureRef, skipping mapping.")
		return nil
	}

	if c.Spec.InfrastructureRef.GroupVersionKind().Kind != "TbmCluster" {
		log.V(4).Info("Cluster has an InfrastructureRef for a different type, skipping mapping.")
		return nil
	}

	log.V(4).Info("Adding request.", "tbmCluster", c.Spec.InfrastructureRef.Name)
	return []ctrl.Request{
		{
			NamespacedName: client.ObjectKey{Namespace: c.Namespace, Name: c.Spec.InfrastructureRef.Name},
		},
	}
}

func (r *TbmClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.TbmCluster{}).
		WithEventFilter(
			predicate.Funcs{
				// Avoid reconciling if the event triggering the reconciliation is related to incremental status updates
				// for TbmCluster resources only
				UpdateFunc: func(e event.UpdateEvent) bool {
					if e.ObjectOld.GetObjectKind().GroupVersionKind().Kind != "TbmCluster" {
						return true
					}

					oldCluster := e.ObjectOld.(*infrav1.TbmCluster).DeepCopy()
					newCluster := e.ObjectNew.(*infrav1.TbmCluster).DeepCopy()

					oldCluster.Status = infrav1.TbmClusterStatus{}
					newCluster.Status = infrav1.TbmClusterStatus{}

					return !reflect.DeepEqual(oldCluster, newCluster)
				},
			},
		).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "error creating controller")
	}

	return controller.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.requeueTbmClusterForUnpausedCluster),
		},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCluster := e.ObjectOld.(*clusterv1.Cluster)
				newCluster := e.ObjectNew.(*clusterv1.Cluster)
				log := r.Log.WithValues("predicate", "updateEvent", "namespace", newCluster.Namespace, "cluster", newCluster.Name)
				switch {
				// return true if Cluster.Spec.Paused has changed from true to false
				case oldCluster.Spec.Paused && !newCluster.Spec.Paused:
					log.V(4).Info("Cluster was unpaused, will attempt to map associated TbmCluster.")
					return true
				// otherwise, return false
				default:
					log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmCluster.")
					return false
				}
			},
			CreateFunc: func(e event.CreateEvent) bool {
				cluster := e.Object.(*clusterv1.Cluster)
				log := r.Log.WithValues("predicate", "createEvent", "namespace", cluster.Namespace, "cluster", cluster.Name)

				// Only need to trigger a reconcile if the Cluster.Spec.Paused is false
				if !cluster.Spec.Paused {
					log.V(4).Info("Cluster is not paused, will attempt to map associated TbmCluster.")
					return true
				}
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmCluster.")
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				log := r.Log.WithValues("predicate", "deleteEvent", "namespace", e.Meta.GetNamespace(), "cluster", e.Meta.GetName())
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmCluster.")
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				log := r.Log.WithValues("predicate", "genericEvent", "namespace", e.Meta.GetNamespace(), "cluster", e.Meta.GetName())
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmCluster.")
				return false
			},
		},
	)
}
