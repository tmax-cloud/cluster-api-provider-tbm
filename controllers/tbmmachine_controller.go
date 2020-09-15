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
	"fmt"
	"reflect"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "cluster-api-provider-tbm/api/v1alpha3"
	infraUtil "cluster-api-provider-tbm/controllers/util"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

// TbmMachineReconciler reconciles a TbmMachine object
type TbmMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

func (r *TbmMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.TODO()
	log = r.Log.WithValues("tbmmachine", req.NamespacedName)

	// your logic here

	// Fetch the TbmMachine instance.
	tbmMachine := &infrav1.TbmMachine{}
	err := r.Get(ctx, req.NamespacedName, tbmMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Machine.
	machine, err := util.GetOwnerMachine(ctx, r.Client, tbmMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("machine", machine.Name)

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, nil
	}

	if util.IsPaused(cluster, tbmMachine) {
		log.Info("TbmMachine or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	if !tbmMachine.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if _, found := machine.Labels["cluster.x-k8s.io/control-plane"]; found {
		log.Info("TbmMachine will be provisioned with Control-Plane")

		return r.controlPlaneReconcileNormal(tbmMachine, machine)
	} else if _, found := machine.Labels["cluster.x-k8s.io/deployment-name"]; found {
		log.Info("TbmMachine will be provisioned with Machine-Deployment")

		return r.machineDeploymentReconcileNormal(tbmMachine)
	}

	return ctrl.Result{}, nil
}

func (r *TbmMachineReconciler) controlPlaneReconcileNormal(tbmMachine *infrav1.TbmMachine, machine *clusterv1.Machine) (ctrl.Result, error) {
	log = r.Log.WithValues("tbmmachine", "default")

	if !tbmMachine.Status.Ready {
		if tbmMachine.Spec.ProviderID == "temp" {
			oldTbmMachine := tbmMachine.DeepCopy()
			newTbmMachine := tbmMachine.DeepCopy()

			newTbmMachine.Spec.ProviderID = fmt.Sprintf("tbm:///%s", newTbmMachine.Name)

			r.Patch(context.TODO(), newTbmMachine, client.MergeFrom(oldTbmMachine))
		} else if len(tbmMachine.Status.Addresses) == 0 {
			//do provisioning & address
			oldTbmMachine := tbmMachine.DeepCopy()
			newTbmMachine := tbmMachine.DeepCopy()

			if address := r.provisionMachine(machine); address != "" {
				newTbmMachine.Status.Addresses = clusterv1.MachineAddresses{clusterv1.MachineAddress{Type: "InternalIP", Address: address}}
				newTbmMachine.Status.Ready = true
			} else {
				return ctrl.Result{}, nil
			}

			r.Status().Patch(context.TODO(), newTbmMachine, client.MergeFrom(oldTbmMachine))
		}

		return ctrl.Result{}, nil
	} else {
		return ctrl.Result{}, nil
	}
}

func (r *TbmMachineReconciler) machineDeploymentReconcileNormal(tbmMachine *infrav1.TbmMachine) (ctrl.Result, error) {
	// do k8s provisioning

	return ctrl.Result{}, nil
}

func (r *TbmMachineReconciler) provisionMachine(machine *clusterv1.Machine) string {
	log = r.Log.WithValues("tbmmachine", "default")

	//get tbmpool for provisioning
	tbm := r.getTbm()

	//do provisioning
	if machine.Spec.Bootstrap.DataSecretName == nil {
		return ""
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: machine.Namespace, Name: *machine.Spec.Bootstrap.DataSecretName}
	r.Get(context.TODO(), key, secret)
	value, _ := secret.Data["value"]

	r.doProvision(tbm.Spec.SSH, value)

	//patch
	oldTbm := tbm.DeepCopy()
	newTbm := tbm.DeepCopy()

	newTbm.Labels[infraUtil.LabelClusterName] = strings.Split(newTbm.Labels[infraUtil.LabelClusterName], infraUtil.LabelClusterNameProvisioning)[0]

	r.Patch(context.TODO(), newTbm, client.MergeFrom(oldTbm))

	return newTbm.Spec.SSH.IP
}

func (r *TbmMachineReconciler) doProvision(sshInfo *infrav1.SSHinfo, value []byte) {
	log = r.Log.WithValues("tbmmachine", "default")

	cloudConfig := infraUtil.NewCloudConfig()
	cloudConfig.FromBytes(value)

	conn, err := infraUtil.Connect(sshInfo.IP+":22", sshInfo.SSHid, sshInfo.SSHpw)
	if err != nil {
		log.Error(err, "sshConnection Error")
	}

	conn.SendCommands("rm -r /etc/kubernetes/pki")
	conn.SendCommands("rm /tmp/kubeadm.yaml")
	conn.SendCommands("mkdir -p /etc/kubernetes/pki")
	conn.SendCommands("mkdir -p /etc/kubernetes/pki/etcd")

	for _, wf := range cloudConfig.WriteFiles {
		contents := ""
		for _, content := range wf.Contents {
			if strings.HasPrefix(content, "  name: ") {
				contents = contents + "  name: master-1"
				continue
			}
			if content == "" || len(content) == 0 {
				continue
			}
			contents = contents + content + "\\n"
		}

		if _, err := conn.SendCommands("echo -e \"" + contents + "\" >> " + wf.Path); err != nil {
			log.Error(err, wf.Path+" writeFileError")
		}
		conn.SendCommands("chmod " + wf.Permissions + " " + wf.Path)
	}

	if output, err := conn.SendCommands(cloudConfig.RunCmd[0]); err != nil {
		log.Error(err, "k8s provisioning ERROR")
	} else {
		log.Info("k8s provisioning result")
		log.Info(string(output))
	}
}

func (r *TbmMachineReconciler) getTbm() *infrav1.TbmPool {
	tbmPoolList := &infrav1.TbmPoolList{}

	listOptions := []client.ListOption{
		client.MatchingLabels(map[string]string{
			infraUtil.LabelValid: "true",
		}),
	}
	r.List(context.TODO(), tbmPoolList, listOptions...)

	return &tbmPoolList.Items[0]
}

// TbmClusterToTbmMachines is a handler.ToRequestsFunc to be used to enqeue requests for reconciliation
// of TbmMachines.
func (r *TbmMachineReconciler) TbmClusterToTbmMachines(o handler.MapObject) []ctrl.Request {
	c := o.Object.(*infrav1.TbmCluster)
	log := r.Log.WithValues("objectMapper", "tbmClusterToTbmMachine", "namespace", c.Namespace, "tbmCluster", c.Name)

	// Don't handle deleted TbmClusters
	if !c.ObjectMeta.DeletionTimestamp.IsZero() {
		log.V(4).Info("TbmCluster has a deletion timestamp, skipping mapping.")
		return nil
	}

	cluster, err := util.GetOwnerCluster(context.TODO(), r.Client, c.ObjectMeta)
	switch {
	case apierrors.IsNotFound(err) || cluster == nil:
		log.V(4).Info("Cluster for TbmCluster not found, skipping mapping.")
		return nil
	case err != nil:
		log.Error(err, "Failed to get owning cluster, skipping mapping.")
		return nil
	}

	return r.requestsForCluster(log, cluster.Namespace, cluster.Name)
}

func (r *TbmMachineReconciler) requeueTbmMachinesForUnpausedCluster(o handler.MapObject) []ctrl.Request {
	c := o.Object.(*clusterv1.Cluster)
	log := r.Log.WithValues("objectMapper", "clusterToTbmMachine", "namespace", c.Namespace, "cluster", c.Name)

	// Don't handle deleted clusters
	if !c.ObjectMeta.DeletionTimestamp.IsZero() {
		log.V(4).Info("Cluster has a deletion timestamp, skipping mapping.")
		return nil
	}

	return r.requestsForCluster(log, c.Namespace, c.Name)
}

func (r *TbmMachineReconciler) requestsForCluster(log logr.Logger, namespace, name string) []ctrl.Request {
	labels := map[string]string{clusterv1.ClusterLabelName: name}
	machineList := &clusterv1.MachineList{}
	if err := r.Client.List(context.TODO(), machineList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		log.Error(err, "Failed to get owned Machines, skipping mapping.")
		return nil
	}

	result := make([]ctrl.Request, 0, len(machineList.Items))
	for _, m := range machineList.Items {
		log.WithValues("machine", m.Name)
		if m.Spec.InfrastructureRef.GroupVersionKind().Kind != "TbmMachine" {
			log.V(4).Info("Machine has an InfrastructureRef for a different type, will not add to reconciliation request.")
			continue
		}
		if m.Spec.InfrastructureRef.Name == "" {
			log.V(4).Info("Machine has an InfrastructureRef with an empty name, will not add to reconciliation request.")
			continue
		}
		log.WithValues("tbmMachine", m.Spec.InfrastructureRef.Name)
		log.V(4).Info("Adding TbmMachine to reconciliation request.")
		result = append(result, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}})
	}
	return result
}

func (r *TbmMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	controller, err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.TbmMachine{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("TbmMachine")),
			},
		).
		Watches(
			&source.Kind{Type: &infrav1.TbmCluster{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: handler.ToRequestsFunc(r.TbmClusterToTbmMachines)},
		).
		WithEventFilter(
			predicate.Funcs{
				// Avoid reconciling if the event triggering the reconciliation is related to incremental status updates
				// for TbmMachine resources only
				UpdateFunc: func(e event.UpdateEvent) bool {
					if e.ObjectOld.GetObjectKind().GroupVersionKind().Kind != "TbmMachine" {
						return true
					}

					oldMachine := e.ObjectOld.(*infrav1.TbmMachine).DeepCopy()
					newMachine := e.ObjectNew.(*infrav1.TbmMachine).DeepCopy()

					oldMachine.Status = infrav1.TbmMachineStatus{}
					newMachine.Status = infrav1.TbmMachineStatus{}

					return !reflect.DeepEqual(oldMachine, newMachine)
				},
			},
		).
		Build(r)
	if err != nil {
		return err
	}

	return controller.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.requeueTbmMachinesForUnpausedCluster),
		},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldCluster := e.ObjectOld.(*clusterv1.Cluster)
				newCluster := e.ObjectNew.(*clusterv1.Cluster)
				log := r.Log.WithValues("predicate", "updateEvent", "namespace", newCluster.Namespace, "cluster", newCluster.Name)

				switch {
				// never return true for a paused Cluster
				case newCluster.Spec.Paused:
					log.V(4).Info("Cluster is paused, will not attempt to map associated TbmMachine.")
					return false
				// return true if Cluster.Status.InfrastructureReady has changed from false to true
				case !oldCluster.Status.InfrastructureReady && newCluster.Status.InfrastructureReady:
					log.V(4).Info("Cluster InfrastructureReady became ready, will attempt to map associated TbmMachine.")
					return true
				// return true if Cluster.Spec.Paused has changed from true to false
				case oldCluster.Spec.Paused && !newCluster.Spec.Paused:
					log.V(4).Info("Cluster was unpaused, will attempt to map associated TbmMachine.")
					return true
				// otherwise, return false
				default:
					log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmMachine.")
					return false
				}
			},
			CreateFunc: func(e event.CreateEvent) bool {
				cluster := e.Object.(*clusterv1.Cluster)
				log := r.Log.WithValues("predicateEvent", "create", "namespace", cluster.Namespace, "cluster", cluster.Name)

				// Only need to trigger a reconcile if the Cluster.Spec.Paused is false and
				// Cluster.Status.InfrastructureReady is true
				if !cluster.Spec.Paused && cluster.Status.InfrastructureReady {
					log.V(4).Info("Cluster is not paused and has infrastructure ready, will attempt to map associated TbmMachine.")
					return true
				}
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmMachine.")
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				log := r.Log.WithValues("predicateEvent", "delete", "namespace", e.Meta.GetNamespace(), "cluster", e.Meta.GetName())
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmMachine.")
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				log := r.Log.WithValues("predicateEvent", "generic", "namespace", e.Meta.GetNamespace(), "cluster", e.Meta.GetName())
				log.V(4).Info("Cluster did not match expected conditions, will not attempt to map associated TbmMachine.")
				return false
			},
		},
	)
}
