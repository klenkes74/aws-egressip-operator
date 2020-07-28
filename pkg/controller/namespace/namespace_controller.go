package namespace

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/klenkes74/egressip-ipam-operator/pkg/cloudprovider"
	"github.com/klenkes74/egressip-ipam-operator/pkg/logger"
	"github.com/klenkes74/egressip-ipam-operator/pkg/openshift"
	ocpnetv1 "github.com/openshift/api/network/v1"
	"github.com/redhat-cop/egressip-ipam-operator/pkg/controller/egressipam"
	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
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

const controllerName = "namespace-controller"
const finalizerName = "egressip-ipam-operator.redhat-cop.io/namespace-handler"

var log = logger.Log.WithName(controllerName)

var _ reconcile.Reconciler = &reconcileNamespace{}

type reconcileNamespace struct {
	util.ReconcilerBase

	cloud   *cloudprovider.CloudProvider
	handler openshift.EgressIPHandler
}

// Add creates a new Namespace Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, cloud *cloudprovider.CloudProvider) error {
	log.Info(fmt.Sprintf("Adding reconciler '%s' to operator manager", controllerName))

	return add(mgr, newReconciler(mgr, cloud))
}

// newReconciler returns a new reconcile.r
func newReconciler(mgr manager.Manager, cloud *cloudprovider.CloudProvider) reconcile.Reconciler {
	return &reconcileNamespace{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllerName)),
		cloud:          cloud,
		handler:        *openshift.NewEgressIPHandler(*cloud, mgr.GetClient()),
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
			_, actionNeeded := e.Meta.GetAnnotations()[egressipam.NamespaceAnnotation]
			return actionNeeded
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			_, okold := e.MetaOld.GetAnnotations()[egressipam.NamespaceAnnotation]
			_, oknew := e.MetaNew.GetAnnotations()[egressipam.NamespaceAnnotation]
			_, ipsold := e.MetaOld.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			_, ipsnew := e.MetaNew.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
			finalizer := e.MetaNew.GetDeletionTimestamp() != nil
			return (okold && !oknew) || (ipsold != ipsnew) || (oknew && !okold) || finalizer
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			_, actionNeeded := e.Meta.GetAnnotations()[egressipam.NamespaceAnnotation]
			return actionNeeded
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	// Watch for changes to primary resource Namespace
	err = c.Watch(&source.Kind{Type: &corev1.Namespace{}}, &handler.EnqueueRequestForObject{}, IsAnnotated)
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a Namespace object and makes changes based on the state read
// and what is in the Namespace.Spec
func (r *reconcileNamespace) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("namespace", request.Name)
	reqLogger.Info("Reconciling Namespace")

	// if the namespace needs to be saved at the end.
	changed := false

	namespace, err := r.handler.LoadNamespace(request.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	netnamespace, err := r.handler.LoadNetNameSpace(request.Name)
	if err != nil {
		reqLogger.Error(err, "can not load NetNamespace to Namespace")
		return reconcile.Result{}, err
	}

	changed, err = r.workOnUpdate(namespace, netnamespace, changed, reqLogger)
	if err != nil {
		reqLogger.Error(err, "did not successfully work on updated namespace")
		return reconcile.Result{}, err
	}

	changed, err = r.workOnDelete(namespace, netnamespace, changed, reqLogger)
	if err != nil {
		reqLogger.Error(err, "did not successfully work on deleted namespace")
		return reconcile.Result{}, err
	}

	if changed {

		err = r.handler.SaveNamespace(namespace)
		if err != nil {
			reqLogger.Error(err, "could not save the namespace")
			return reconcile.Result{}, err
		}

		err = r.handler.SaveNetNameSpace(netnamespace)
		if err != nil {
			reqLogger.Error(err, "could not save the netnamespace. The data will be inconsistent")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *reconcileNamespace) workOnUpdate(instance *corev1.Namespace, netnamespace *ocpnetv1.NetNamespace, changed bool, reqLogger logr.Logger) (bool, error) {
	if util.IsBeingDeleted(instance) {
		// I have to do nothing ...
		return changed, nil
	}

	ipString, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	if found && len(ipString) >= 7 { // "1.1.1.1" is the shortes IP possible
		reqLogger.Info("IPs defined to use:",
			"ips", ipString,
		)

		netnamespaceIP, found := netnamespace.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
		if found && netnamespaceIP != ipString {
			reqLogger.Info("the IPs have changed",
				"old-ips", ipString,
				"new-ips", netnamespaceIP,
			)
			r.removeAnnotationFromNetnamespace(netnamespace)
			r.removeFinalizer(instance, reqLogger)
		} else {
			reqLogger.Info("the IPs on netnamespace and namespace are the same. Nothing to do",
				"ips", ipString,
			)
			return changed, nil
		}
	} else {
		reqLogger.Info("no IPs found. Will use random ones ...")
	}

	_, found = instance.GetAnnotations()[egressipam.NamespaceAnnotation]
	if !found {
		reqLogger.Info("egressIP has been removed")

		r.removeAnnotationFromNetnamespace(netnamespace)
		r.removeFinalizer(instance, reqLogger)
	} else {
		reqLogger.Info("eggressIP to configure",
			"ips", ipString,
		)
		ips, err := r.addIPs(instance, netnamespace)
		if err != nil {
			return changed, err
		}

		r.addFinalizer(instance, reqLogger)

		reqLogger.Info("added ips",
			"ips", ips)
	}

	return true, nil
}

// addIPs -- adds random new IPs to the cluster and returns the assigned IPs as result
func (r *reconcileNamespace) addIPs(instance *corev1.Namespace, netnamespace *ocpnetv1.NetNamespace) ([]*net.IP, error) {
	// map[string]*net.IP
	ips, err := r.handler.AddIPsToInfrastructure(instance)
	if err != nil {
		return nil, err
	}

	r.addAnnotationToNamespace(instance, ips)
	r.addAnnotationToNetnamespace(netnamespace, ips)

	return ips, nil
}

// addAnnotationToNamespace -- adds the IP list annotation to the namespace. The namespace needs to be saved after that.
func (r *reconcileNamespace) addAnnotationToNamespace(instance *corev1.Namespace, ips []*net.IP) {
	annotations := instance.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}
	annotations[egressipam.NamespaceAssociationAnnotation] = r.ipsToString(ips)
	instance.SetAnnotations(annotations)
}

func (r *reconcileNamespace) ipsToString(ips []*net.IP) string {
	ipStrings := make([]string, len(ips))
	for i, ip := range ips {
		ipStrings[i] = ip.String()
	}

	return strings.Join(ipStrings, ",")
}

// removed the IP list annotation from the namespace. The namespace needs to be saved after that.
func (r *reconcileNamespace) removeAnnotationFromNamespace(instance *corev1.Namespace) {
	_, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	_, found2 := instance.GetAnnotations()[egressipam.NamespaceAnnotation]
	if found || found2 {
		annotations := instance.GetAnnotations()

		newAnnotations := make(map[string]string, r.calculatenewAnnotationLength(found, found2, annotations))

		for key, value := range annotations {
			if key != egressipam.NamespaceAssociationAnnotation && key != egressipam.NamespaceAnnotation {
				newAnnotations[key] = value
			}
		}
		instance.SetAnnotations(newAnnotations)
	}
}

func (r *reconcileNamespace) calculatenewAnnotationLength(found bool, found2 bool, annotations map[string]string) int {
	var newLen int
	if found && found2 {
		newLen = len(annotations) - 2
	} else {
		newLen = len(annotations) - 1
	}
	return newLen
}

// sets the annotations to a Netnamespace. The Netnamespace will be loaded and saved.
func (r *reconcileNamespace) addAnnotationToNetnamespace(instance *ocpnetv1.NetNamespace, ips []*net.IP) {
	annotations := instance.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string, 1)
	}

	r.removeAnnotationFromNetnamespace(instance)

	annotations[egressipam.NamespaceAssociationAnnotation] = r.ipsToString(ips)
	instance.SetAnnotations(annotations)
}

func (r *reconcileNamespace) removeAnnotationFromNetnamespace(instance *ocpnetv1.NetNamespace) {
	_, found := instance.GetAnnotations()[egressipam.NamespaceAssociationAnnotation]
	_, found2 := instance.GetAnnotations()[egressipam.NamespaceAnnotation]
	if found || found2 {
		annotations := instance.GetAnnotations()

		newAnnotations := make(map[string]string, r.calculatenewAnnotationLength(found, found2, annotations))

		for key, value := range annotations {
			if key != egressipam.NamespaceAssociationAnnotation && key != egressipam.NamespaceAnnotation {
				newAnnotations[key] = value
			}
		}
		instance.SetAnnotations(newAnnotations)
	}
}

func (r *reconcileNamespace) workOnDelete(instance *corev1.Namespace, netNamespace *ocpnetv1.NetNamespace, changed bool, reqLogger logr.Logger) (bool, error) {
	if !util.IsBeingDeleted(instance) {
		return changed, nil
	}

	reqLogger.Info("deleting namespace")

	r.removeIpsFromNamespace(instance, netNamespace)
	r.removeFinalizer(instance, reqLogger)

	return true, nil
}

func (r *reconcileNamespace) removeIpsFromNamespace(instance *corev1.Namespace, netnamespace *ocpnetv1.NetNamespace) {
	r.removeAnnotationFromNamespace(instance)
	r.removeAnnotationFromNetnamespace(netnamespace)
}

// adds the finalizer for egressip handling to the list of finalizers if it is not already listed.
func (r *reconcileNamespace) addFinalizer(instance *corev1.Namespace, reqLogger logr.Logger) {
	reqLogger.Info("adding finalizer to namespace")

	found := false
	for _, f := range instance.Finalizers {
		if f == finalizerName {
			found = true
		}
	}
	if !found {
		instance.Finalizers = append(instance.Finalizers, finalizerName)
	}
}

// removes the finalizer from the list of finalizers on the namespace
func (r *reconcileNamespace) removeFinalizer(instance *corev1.Namespace, reqLogger logr.Logger) {
	reqLogger.Info("removing finalizer from namespace")

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
