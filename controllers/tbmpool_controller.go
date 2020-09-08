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
	"net"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrav1 "cluster-api-provider-tbm/api/v1alpha3"
	infraUtil "cluster-api-provider-tbm/controllers/util"

	"golang.org/x/crypto/ssh"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TbmPoolReconciler reconciles a TbmPool object
type TbmPoolReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=tbmpools/status,verbs=get;update;patch

func (r *TbmPoolReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.TODO()
	log := r.Log.WithValues("tbmpool", req.NamespacedName)

	// your logic here
	tbmPool := &infrav1.TbmPool{}
	err := r.Get(ctx, req.NamespacedName, tbmPool)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	//init: by check proper labels
	if !r.initTbmPool(*tbmPool) {
		log.Info(tbmPool.Name + " init")
		return reconcile.Result{}, nil
	}

	//check: ssh information is valid
	if ok, _ := strconv.ParseBool(tbmPool.Labels[infraUtil.LabelValid]); !ok {
		if err := checkSSHvalid(tbmPool.Spec.SSH); err == nil {
			oldTbmPool := tbmPool.DeepCopy()
			newTbmPool := tbmPool.DeepCopy()

			newTbmPool.Labels[infraUtil.LabelValid] = "true"
			_ = r.Patch(ctx, newTbmPool, client.MergeFrom(oldTbmPool))

			log.Info(tbmPool.Name + " has valid sshInfo. Now it will be used as tbmMachine")
		} else {
			log.Info(tbmPool.Name + " has invalid sshInfo. Please check ssh information")
		}

		return reconcile.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *TbmPoolReconciler) initTbmPool(tbmPool infrav1.TbmPool) bool {
	result := true
	oldTbmPool := tbmPool.DeepCopy()
	newTbmPool := tbmPool.DeepCopy()

	if oldTbmPool.Labels == nil {
		newTbmPool.Labels = map[string]string{
			infraUtil.LabelValid:       "false",
			infraUtil.LabelClusterName: "",
			infraUtil.LabelClusterRole: "",
		}
		result = false
	}

	if _, found := oldTbmPool.Labels[infraUtil.LabelValid]; !found {
		newTbmPool.Labels[infraUtil.LabelValid] = "false"
		result = false
	}
	if _, found := oldTbmPool.Labels[infraUtil.LabelClusterName]; !found {
		newTbmPool.Labels[infraUtil.LabelClusterName] = ""
		result = false
	}
	if _, found := oldTbmPool.Labels[infraUtil.LabelClusterRole]; !found {
		newTbmPool.Labels[infraUtil.LabelClusterRole] = ""
		result = false
	}

	_ = r.Patch(context.TODO(), newTbmPool, client.MergeFrom(oldTbmPool))
	return result
}

func checkSSHvalid(sshInfo *infrav1.SSHinfo) error {
	config := &ssh.ClientConfig{
		User: sshInfo.SSHid,
		Auth: []ssh.AuthMethod{
			ssh.Password(sshInfo.SSHpw),
		},
		HostKeyCallback: ssh.HostKeyCallback(func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }),
	}

	client, err := ssh.Dial("tcp", sshInfo.IP+":22", config)
	if err != nil {
		return err
	}
	defer client.Close()

	return nil
}

func (r *TbmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.TbmPool{}).
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
		Complete(r)
}
