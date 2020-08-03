package netnamespace

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/klenkes74/egressip-ipam-operator/pkg/cloudprovider"
	"github.com/klenkes74/egressip-ipam-operator/pkg/logger"
	"github.com/klenkes74/egressip-ipam-operator/pkg/observability"
	"github.com/klenkes74/egressip-ipam-operator/pkg/openshift"
	v1 "github.com/openshift/api/network/v1"
	"github.com/redhat-cop/egressip-ipam-operator/pkg/controller/egressipam"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"k8s.io/apimachinery/pkg/api/errors"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"
)

const controllerName = "netnamespace-controller"
const finalizerName = "egressip-ipam-operator.redhat-cop.io/netnamespace-handler"

var log = logger.Log.WithName(controllerName)

var _ reconcile.Reconciler = &reconcileNetnamespace{}

type reconcileNetnamespace struct {
	util.ReconcilerBase

	cloud    *cloudprovider.CloudProvider
	handler  openshift.EgressIPHandler
	alarming observability.AlarmStore
}

// Add creates a new Namespace Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, cloud *cloudprovider.CloudProvider) error {
	log.Info(fmt.Sprintf("Adding reconciler '%s' to operator manager", controllerName))
	return add(mgr, newReconciler(mgr, cloud))
}

// newReconciler returns a new reconcile.r
func newReconciler(mgr manager.Manager, cloud *cloudprovider.CloudProvider) reconcile.Reconciler {
	return &reconcileNetnamespace{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllerName)),
		cloud:          cloud,
		handler:        *openshift.NewEgressIPHandler(*cloud, mgr.GetClient()),
		alarming:       *observability.NewAlarmStore(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.r
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	IsAnnotated := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			_, actionNeeded := e.Meta.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			return actionNeeded
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldips, okold := e.MetaOld.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			newips, oknew := e.MetaNew.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			return (okold && !oknew) || (!okold && oknew) || (okold != oknew) || (oldips != newips)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, actionNeeded := e.Meta.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			return actionNeeded
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	// Watch for changes to primary resource Namespace
	err = c.Watch(&source.Kind{Type: &v1.NetNamespace{}}, &handler.EnqueueRequestForObject{}, IsAnnotated)
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a Namespace object and makes changes based on the state read
// and what is in the Namespace.Spec
func (r *reconcileNetnamespace) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("netnamespace", request.Name)
	reqLogger.Info("Reconciling Netnamespace")

	// Fetch the Namespace instance
	instance := &v1.NetNamespace{}
	err := r.GetClient().Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.

		r.alarming.AddAlarm(request.Name, []*net.IP{})
		return reconcile.Result{}, err
	}

	changed := false
	changed, err = r.workOnUpdate(instance, changed, reqLogger)
	if err != nil {
		reqLogger.Error(err, "could not work on updated Netnamespace")
		return reconcile.Result{}, err
	}

	changed, err = r.workOnDelete(instance, changed, reqLogger)
	if err != nil {
		reqLogger.Error(err, "could not work on deleted Netnamespace")
		return reconcile.Result{}, err
	}

	if changed {
		reqLogger.Info("saving the changed netnamespace")

		err = r.GetClient().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "could not save the netnamespace")

		}
	} else {
		reqLogger.Info("Nothing to do for the netnamespace")
	}

	return reconcile.Result{}, nil
}

func (r *reconcileNetnamespace) workOnUpdate(instance *v1.NetNamespace, changed bool, reqLogger logr.Logger) (bool, error) {
	if util.IsBeingDeleted(instance) { // the namespace is deleted and we need to clean up.
		return false, nil
	}

	var err error
	oldips := instance.EgressIPs
	newips := r.getAnnotatedIPs(instance)

	if r.matchingIPs(oldips, newips) {
		reqLogger.Info("nothing changed - ips stayed the same",
			"ips.old", oldips,
			"ips.new", newips,
		)
		return changed, nil
	}

	reqLogger.Info("update netnamespace",
		"ips.old", oldips,
		"ips.new", newips,
	)

	if len(instance.EgressIPs) > 0 {
		err = r.removeIpsFromNetnamespace(instance)
		if err != nil {
			return changed, err
		}

		reqLogger.Info("removed old ips",
			"ips.old", oldips,
		)
		changed = true
	}

	if len(newips) > 0 {
		r.addIPsToAnnotation(instance, newips)
		err = r.addSpecifiedIPsToNamespace(instance, reqLogger)
		if err != nil {
			r.alarming.AddAlarm(instance.Name, newips)

			return changed, err
		}

		reqLogger.Info("added new ips",
			"ips.new", newips,
		)

		r.alarming.RemoveAlarm(instance.Name)
		changed = true
	} else {
		r.removeFinalizer(instance)
	}
	reqLogger.Info("finished updating the netnamespace")
	return changed, nil
}

func (r *reconcileNetnamespace) matchingIPs(a []string, b []*net.IP) bool {
	if len(a) != len(b) {
		return false
	}

	resultSet := make([]string, 0)
	for _, s := range a {
		for _, s2 := range b {
			if s == s2.String() {
				resultSet = append(resultSet, s)
			}
		}
	}
	return len(resultSet) == len(a)
}

func (r *reconcileNetnamespace) workOnDelete(instance *v1.NetNamespace, changed bool, reqLogger logr.Logger) (bool, error) {
	if !util.IsBeingDeleted(instance) {
		return changed, nil
	}

	reqLogger.Info("deleting netnamespace")

	r.removeFinalizer(instance)

	return true, r.removeIpsFromNetnamespace(instance)
}

// add the specified IPs to the cluster to be usable as egress ips
func (r *reconcileNetnamespace) addSpecifiedIPsToNamespace(instance *v1.NetNamespace, reqLogger logr.Logger) error {
	ips := r.getAnnotatedIPs(instance)
	reqLogger.Info("Adding specified IPs to netnamespace",
		"ips", ips,
	)

	err := r.handler.AddIPsToNetNamespace(instance, ips)
	if err != nil {
		r.alarming.AddAlarm(instance.Name, ips)

		reqLogger.Error(err, "could not assign IPs to Netnamespace")
		return err
	}

	r.addFinalizer(instance)

	return nil
}

// returns the IPs that are annotated to be used for the egress ips.
func (r *reconcileNetnamespace) getAnnotatedIPs(instance *v1.NetNamespace) []*net.IP {
	var ips []*net.IP
	ipstring, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	if found && len(ipstring) > 0 {
		ipStrings := strings.Split(ipstring, ",")
		ips = make([]*net.IP, len(ipStrings))

		for i, ip := range ipStrings {
			ipStruct := net.ParseIP(ip)
			ips[i] = &ipStruct
		}
	}
	return ips
}

func (r *reconcileNetnamespace) addIPsToAnnotation(instance *v1.NetNamespace, ips []*net.IP) {
	ipStrings := make([]string, len(ips))
	for i, newip := range ips {
		ipStrings[i] = newip.String()
	}
	annotations := instance.GetAnnotations()
	annotations[egressipam.NamespaceAssociationAnnotation] = strings.Join(ipStrings, ",")
	instance.SetAnnotations(annotations)
}

func (r *reconcileNetnamespace) removeIpsFromNetnamespace(instance *v1.NetNamespace) error {
	err := r.handler.RemoveIPsFromInfrastructure(instance)
	if err != nil {
		return err
	}

	r.handler.RemoveIPsFromNetNamespace(instance)
	r.removeFinalizer(instance)

	_, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	if found {
		annotations := instance.GetAnnotations()
		newAnnotations := make(map[string]string, len(annotations)-1)

		for key, value := range annotations {
			if key != egressipam.NamespaceAssociationAnnotation {
				newAnnotations[key] = value
			}
		}
		instance.SetAnnotations(newAnnotations)
	}

	return nil
}

// Adds the finalizer to the netnamespace
func (r *reconcileNetnamespace) addFinalizer(instance *v1.NetNamespace) {
	found := false
	for _, f := range instance.Finalizers {
		if f == finalizerName {
			found = true

			break
		}
	}
	if !found {
		instance.Finalizers = append(instance.Finalizers, finalizerName)
	}
}

// Removes the finalizer from the netnamespace.
func (r *reconcileNetnamespace) removeFinalizer(instance *v1.NetNamespace) {
	finalizers := instance.GetFinalizers()

	for i, f := range finalizers {
		if f == finalizerName {
			finalizers[i] = finalizers[len(finalizers)-1]
			finalizers[len(finalizers)-1] = ""
			finalizers = finalizers[:len(finalizers)-1]

			instance.SetFinalizers(finalizers)
			break
		}
	}
}
