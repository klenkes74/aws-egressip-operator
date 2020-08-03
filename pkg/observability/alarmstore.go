package observability

import (
	"github.com/klenkes74/egressip-ipam-operator/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
	"net"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"time"
)

var log = logger.Log.WithName("alarmstore")

// AlarmStore -- the store for keeping alarms of the aws-egressip-operator
type AlarmStore interface {
	// Adds a failed namespace to the alarm store
	AddAlarm(namespace string, ips []*net.IP)

	// Removes a recovered namespace from the alarm store
	RemoveAlarm(namespace string)

	RemoveAlarmForIP(namespace string, ip *net.IP)

	// Retrieves all failed namespaces from the alarm store
	GetFailed() map[string]*FailedEgressIP
}

// ensures that the PrometheusLinkedAlarmStore is a valid AlarmStore
var _ AlarmStore = &PrometheusLinkedAlarmStore{}

// PrometheusLinkedAlarmStore -- a simple in memory implementation of the AlarmStore
type PrometheusLinkedAlarmStore struct {
	failures map[string]*FailedEgressIP
	counter  prometheus.GaugeVec
}

var singletonAlarmStore *PrometheusLinkedAlarmStore

// NewAlarmStore -- creates the default implementation of the alarm store
func NewAlarmStore() *AlarmStore {
	if singletonAlarmStore == nil {
		createAlarmStore()
	}

	result := AlarmStore(singletonAlarmStore)

	return &result
}

func createAlarmStore() {
	counter := *prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "egressip",
			Name:      "handling_failures",
			Help:      "Failures while handling egressips",
		},
		[]string{"namespace"},
	)
	err := metrics.Registry.Register(counter)
	if err != nil {
		log.Error(err, "Can't register the new gauge")
	}

	singletonAlarmStore = &PrometheusLinkedAlarmStore{
		failures: make(map[string]*FailedEgressIP),
		counter:  counter,
	}
}

// AddAlarm -- Adds a failed namespace to the alarm store
func (s PrometheusLinkedAlarmStore) AddAlarm(namespace string, ips []*net.IP) {
	if s.failures[namespace] == nil {
		timeStamp := time.Now()

		alarm := FailedEgressIP{
			Namespace:      namespace,
			FailedIPs:      ips,
			FirstOccurance: timeStamp,
			LastOccurance:  timeStamp,
			Counter:        float64(1),
		}

		s.failures[namespace] = &alarm

	} else {
		s.failures[namespace].Counter = s.failures[namespace].Counter + 1
		s.failures[namespace].LastOccurance = time.Now()
		s.failures[namespace].FailedIPs = ips
	}

	s.counter.WithLabelValues(namespace).Set(s.failures[namespace].Counter)
}

// RemoveAlarm -- Removes a recovered namespace from the alarm store
func (s PrometheusLinkedAlarmStore) RemoveAlarm(namespace string) {
	if s.failures[namespace] != nil {
		s.counter.WithLabelValues(namespace).Set(0)

		delete(s.failures, namespace)
	}
}

// RemoveAlarmForIP -- Removes the alarm for a single IP. If there are still IPs in alarm, keep the alarm, if that has
// been the last IP, remove the alarm.
func (s PrometheusLinkedAlarmStore) RemoveAlarmForIP(namespace string, ip *net.IP) {
	if s.failures[namespace] != nil {
		newFailures := make([]*net.IP, 0)

		for _, oldIP := range s.failures[namespace].FailedIPs {
			if !reflect.DeepEqual(oldIP, ip) {
				newFailures = append(newFailures, oldIP)
			}
		}

		if len(newFailures) > 0 {
			s.failures[namespace].FailedIPs = newFailures
		} else {
			s.RemoveAlarm(namespace)
		}
	}
}

// GetFailed -- Retrieves all failed namespaces from the alarm store
func (s PrometheusLinkedAlarmStore) GetFailed() map[string]*FailedEgressIP {
	return s.failures
}

// FailedEgressIP - This is the data for the failure.
type FailedEgressIP struct {
	Namespace      string    // The failed namespace
	FailedIPs      []*net.IP // The failed IPs
	FirstOccurance time.Time // First occurance of this failure
	LastOccurance  time.Time // Last occurance of this failure
	Counter        float64   // Failure counter
}
