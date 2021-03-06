package hostsubnet

//goland:noinspection SpellCheckingInspection
import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/klenkes74/aws-egressip-operator/pkg/cloudprovider"
	"github.com/klenkes74/aws-egressip-operator/pkg/logger"
	"github.com/klenkes74/aws-egressip-operator/pkg/observability"
	"github.com/klenkes74/aws-egressip-operator/pkg/openshift"
	corev1 "github.com/openshift/api/network/v1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//goland:noinspection SpellCheckingInspection
const controllerName = "hostsubnet-controller"

//goland:noinspection SpellCheckingInspection
const finalizerName = "egressip-ipam-operator.redhat-cop.io/hostsubnet-handler"

var log = logger.Log.WithName(controllerName)

var _ reconcile.Reconciler = &reconcileHostSubnet{}

//goland:noinspection SpellCheckingInspection
type reconcileHostSubnet struct {
	util.ReconcilerBase

	cloud    cloudprovider.CloudProvider
	handler  openshift.EgressIPHandler
	alarming observability.AlarmStore
}

// Add creates a new Namespace Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, cloud *cloudprovider.CloudProvider) error {
	log.Info(fmt.Sprintf("Adding reconciler '%s' to operator manager", controllerName))
	return add(mgr, newReconciler(mgr, cloud))
}

// newReconciler returns a new reconcile.
func newReconciler(mgr manager.Manager, cloud *cloudprovider.CloudProvider) reconcile.Reconciler {
	return &reconcileHostSubnet{
		ReconcilerBase: util.NewReconcilerBase(mgr.GetClient(), mgr.GetScheme(), mgr.GetConfig(), mgr.GetEventRecorderFor(controllerName)),
		cloud:          *cloud,
		handler:        *openshift.NewEgressIPHandler(*cloud, *openshift.NewOcpClient(mgr.GetClient())),
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

	needsReconciliation := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return util.IsBeingDeleted(e.MetaNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	// Watch for changes to primary resource Namespace
	err = c.Watch(&source.Kind{Type: &corev1.HostSubnet{}}, &handler.EnqueueRequestForObject{}, needsReconciliation)
	if err != nil {
		return err
	}

	return nil
}

// Reconcile - the workhorse of the operator ...
func (r *reconcileHostSubnet) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("instance", request.Name)
	reqLogger.Info("Reconciling hostSubnet")

	instance, done, err := r.loadHostSubnet(request.NamespacedName, reqLogger)
	if done {
		reqLogger.Info("stop reconcilation of hostSubnet")
		return reconcile.Result{}, nil
	}

	if err != nil {
		reqLogger.Error(err, "other error - reconcile again")
		return reconcile.Result{}, err
	}

	var changed bool

	if !util.IsBeingDeleted(instance) {
		changed, err = r.updateHostSubnet(instance, reqLogger, changed)
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		changed, err = r.deleteHostSubnet(instance, reqLogger, changed)
		if err != nil {
			for _, ip := range instance.EgressIPs {
				namespace := instance.GetAnnotations()[openshift.IPToNamespaceAnnotation+ip]
				alarmIP := net.ParseIP(ip)
				r.alarming.AddAlarm(namespace, []*net.IP{&alarmIP})
			}

			return reconcile.Result{}, err
		}
	}

	if changed {
		err = r.handler.SaveHostSubnet(instance)
		if err != nil {
			reqLogger.Error(err, "could not save hostSubnet")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (r *reconcileHostSubnet) updateHostSubnet(instance *corev1.HostSubnet, reqLogger logr.Logger, changed bool) (bool, error) {
	ips := r.handler.ReadIpsFromHostSubnet(instance)

	changed = r.addFinalizer(instance, reqLogger) || changed

	err := r.handler.CheckIPsForHost(instance, ips)
	if err != nil {
		reqLogger.Error(err, "problems with IPs. need to redistribute IPs")
		_, err = r.handler.RedistributeIPsFromHost(instance)

		if err != nil {
			r.raiseAlarmForIPs(instance, ips)
		} else {
			r.cancelAlarmforIPs(instance, ips)
		}

		changed = true
	}

	return changed, err
}

func (r *reconcileHostSubnet) loadHostSubnet(name types.NamespacedName, reqLogger logr.Logger) (*corev1.HostSubnet, bool, error) {
	// Fetch the Namespace instance
	instance, err := r.handler.LoadHostSubnet(name.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Error(err, "can not find the object. Is already deleted. Don't requeue this request")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return nil, true, nil
		}

		reqLogger.Error(err, "can not load the object. Requeue the request")
		// Error reading the object - requeue the request.
		return nil, false, err
	}

	if util.IsBeingDeleted(instance) && !util.HasFinalizer(instance, finalizerName) {
		reqLogger.Info("deleted object has no finalizer - ignoring it.")
		return instance, true, nil
	}

	return instance, false, nil
}

func (r *reconcileHostSubnet) deleteHostSubnet(instance *corev1.HostSubnet, reqLogger logr.Logger, changed bool) (bool, error) {
	ips := r.handler.ReadIpsFromHostSubnet(instance)

	reqLogger.Info("reconciling deleted host subnet",
		"ips", ips,
	)

	if len(ips) == 0 {
		reqLogger.Info("no ipaddresses to redistribute from hostsubnet")
	} else {
		reqLogger.Info("Redistributing ipaddresses of hostsubnet",
			"ips", ips,
		)

		distribution, err := r.handler.RedistributeIPsFromHost(instance)
		if err != nil {
			reqLogger.Error(err,
				"redistribution of IPs failed. Egress networking will cease working for projects if the other hosts are also failing",
				"hostname", instance.Name,
				"egress-ips", ips,
			)

			r.raiseAlarmForIPs(instance, ips)

			return changed, err
		}

		r.cancelAlarmforIPs(instance, ips)
		for ip, host := range distribution {
			reqLogger.Info("redistributed IP",
				"ip", ip,
				"host", host,
			)
		}
	}

	r.removeFinalizer(instance, reqLogger)
	return true, nil
}

// adds the finalizer for egressip handling to the list of finalizers if it is not already listed.
func (r *reconcileHostSubnet) addFinalizer(instance *corev1.HostSubnet, reqLogger logr.Logger) bool {
	found := util.HasFinalizer(instance, finalizerName)

	if !found {
		reqLogger.Info("adding finalizer to hostSubnet")
		util.AddFinalizer(instance, finalizerName)
	}

	return !found
}

// removes the finalizer from the list of finalizers on the node
func (r *reconcileHostSubnet) removeFinalizer(instance *corev1.HostSubnet, reqLogger logr.Logger) bool {
	result := util.HasFinalizer(instance, finalizerName)

	if result {
		reqLogger.Info("removing finalizer from hostSubnet")
		util.RemoveFinalizer(instance, finalizerName)
	}

	return result
}

func (r *reconcileHostSubnet) raiseAlarmForIPs(instance *corev1.HostSubnet, ips []*net.IP) {
	for _, ip := range ips {
		namespace := instance.GetAnnotations()[openshift.IPToNamespaceAnnotation+ip.String()]
		r.alarming.AddAlarm(namespace, []*net.IP{ip})
	}
}

func (r *reconcileHostSubnet) cancelAlarmforIPs(instance *corev1.HostSubnet, ips []*net.IP) {
	for _, ip := range ips {
		namespace := instance.GetAnnotations()[openshift.IPToNamespaceAnnotation+ip.String()]
		r.alarming.RemoveAlarmForIP(namespace, ip)
	}
}
