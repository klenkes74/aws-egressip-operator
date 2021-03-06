package main

import (
	"context"
	"fmt"
	"github.com/klenkes74/aws-egressip-operator/pkg/logger"
	kubemetrics "github.com/operator-framework/operator-sdk/pkg/kube-metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"time"

	"runtime"

	"github.com/klenkes74/aws-egressip-operator/pkg/controller"
	"github.com/klenkes74/aws-egressip-operator/version"
	ocpnetv1 "github.com/openshift/api/network/v1"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	"github.com/operator-framework/operator-sdk/pkg/leader"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"github.com/redhat-cop/egressip-ipam-operator/pkg/apis"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
)

const lockName = "aws-egressip-operator-lock"

var log = logger.Log

// Change below variables to serve metrics on different host or port.
var (
	metricsHost       = "0.0.0.0"
	metricsPort int32 = 8081
)

func main() {
	cfg, ctx := initializeSystem()
	becomeLeader(ctx, lockName)

	namespace := getWatchNamespace()

	mgr := createManager(cfg, namespace)
	registerComponents(mgr)
	setupControllers(mgr)
	_ = serveCRMetrics(cfg, namespace)
	startManager(mgr)
}

func initializeSystem() (*rest.Config, context.Context) {
	printVersion()

	return initializeAPIServer()
}

func printVersion() {
	log.Info(fmt.Sprintf("Operator Version: %s", version.Version))
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("Version of operator-sdk: %v", sdkVersion.Version))
}

func initializeAPIServer() (*rest.Config, context.Context) {
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "cant load the configuration")
		os.Exit(2)
	}

	ctx := context.TODO()
	return cfg, ctx
}

func getWatchNamespace() string {
	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Error(err, "Failed to get watch namespace")
		os.Exit(3)
	}

	return namespace
}

// Become the leader before proceeding
func becomeLeader(ctx context.Context, lockname string) {
	master := false
	for !master {
		err := leader.Become(ctx, lockname)
		if err != nil {
			log.Info("Master already chosen. Wait for 10 seconds ...", "lockname", lockname)
			time.Sleep(10 * time.Second)
		} else {
			master = true
		}
	}
}

func createManager(cfg *rest.Config, namespace string) manager.Manager {
	// Create a new Cmd to provide shared dependencies and start components
	metricsBindAddress := fmt.Sprintf("%s:%d", metricsHost, metricsPort)
	log.Info("Starting kubemetrics",
		"metricsBindAddress", metricsBindAddress,
	)
	mgr, err := manager.New(cfg, manager.Options{
		Namespace:          namespace,
		MetricsBindAddress: metricsBindAddress,
		Host:               metricsHost,
		Port:               int(metricsPort),
	})
	if err != nil {
		log.Error(err, "Can't create manager")
		os.Exit(4)
	}
	return mgr
}

func registerComponents(mgr manager.Manager) {
	log.Info("Registering Components.")

	// Setup Scheme for all resources
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "can't add manager scheme to APIs")
		os.Exit(5)
	}

	if err := corev1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "can't add k8s core scheme")
		os.Exit(6)
	}

	if //noinspection GoDeprecation
	err := ocpnetv1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "can't add k8s network scheme")
		os.Exit(7)
	}
}

func setupControllers(mgr manager.Manager) {
	// Setup all Controllers
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "setup controllers failed")
		os.Exit(8)
	}
}

func startManager(mgr manager.Manager) {
	log.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "Manager exited non-zero")
		os.Exit(9)
	}
}

// serveCRMetrics gets the Operator/CustomResource GVKs and generates metrics based on those types.
// It serves those metrics on "http://metricsHost:operatorMetricsPort".
func serveCRMetrics(cfg *rest.Config, operatorNs string) error {
	namespaceType := &corev1.Namespace{}
	netNamespaceType := &ocpnetv1.NetNamespace{}
	nodeType := &corev1.Node{}
	hostSubnetType := &ocpnetv1.HostSubnet{}

	filteredGVK := []schema.GroupVersionKind{
		namespaceType.GroupVersionKind(),
		netNamespaceType.GroupVersionKind(),
		nodeType.GroupVersionKind(),
		hostSubnetType.GroupVersionKind(),
	}

	// The metrics will be generated from the namespaces which are returned here.
	// NOTE that passing nil or an empty list of namespaces in GenerateAndServeCRMetrics will result in an error.
	err := kubemetrics.GenerateAndServeCRMetrics(cfg, []string{operatorNs}, filteredGVK, metricsHost, metricsPort)
	if err != nil {
		return err
	}

	return nil
}
