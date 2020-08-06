package main

import (
	"github.com/klenkes74/aws-egressip-operator/pkg/observability"
	"net"
	"reflect"
	"testing"
	"time"
)

var expectedNamespace = "test"
var expectedIPs []*net.IP

func prepareStore() (observability.AlarmStore, time.Time) {
	store := *observability.NewAlarmStore()

	expectedIPs = make([]*net.IP, 2)
	ip1 := net.ParseIP("1.1.1.1")
	expectedIPs[0] = &ip1
	ip2 := net.ParseIP("2.2.2.2")
	expectedIPs[1] = &ip2

	store.AddAlarm(expectedNamespace, expectedIPs)

	failed := store.GetFailed()
	return store, failed[expectedNamespace].FirstOccurance
}

func TestAddingNamespaceToAlarmStore(t *testing.T) {
	store, _ := prepareStore()

	failed := store.GetFailed()

	for _, failedEgress := range failed {
		if failedEgress.Namespace != "test" {
			t.Errorf("Namespace name does not match! expected='%v', current='%v'",
				expectedNamespace,
				failedEgress.Namespace,
			)
		}

		if !reflect.DeepEqual(failedEgress.FailedIPs, expectedIPs) {
			t.Errorf("IPs don't match! expected='%v', current='%v'",
				expectedIPs,
				failedEgress.FailedIPs,
			)
		}
	}

	store.RemoveAlarm(expectedNamespace)
}

func TestRemovingNamespaceFromAlarmStore(t *testing.T) {
	store, _ := prepareStore()

	store.RemoveAlarm(expectedNamespace)

	if len(store.GetFailed()) > 0 {
		t.Error("There should be no failures in the AlarmStore!")
	}
}

func TestRemovingAlarmForSingleIP(t *testing.T) {
	store, _ := prepareStore()

	ip := net.ParseIP("2.2.2.2")
	store.RemoveAlarmForIP(expectedNamespace, &ip)

	if len(store.GetFailed()[expectedNamespace].FailedIPs) > 1 {
		t.Errorf("There should be only one IP listed as failed ip! expected=1, current=%v", len(store.GetFailed()[expectedNamespace].FailedIPs))
	}

	store.RemoveAlarm(expectedNamespace)
}

func TestRemovingAlarmForLastSingleIP(t *testing.T) {
	store, _ := prepareStore()

	ip := net.ParseIP("2.2.2.2")
	store.RemoveAlarmForIP(expectedNamespace, &ip)

	ip = net.ParseIP("1.1.1.1")
	store.RemoveAlarmForIP(expectedNamespace, &ip)

	if len(store.GetFailed()) > 0 {
		t.Errorf("There should be no failure any more! expected=0, current=%v", len(store.GetFailed()))
	}

	store.RemoveAlarm(expectedNamespace)
}

func TestGetFirstAndLastOccurrenceFromAlarm(t *testing.T) {
	store, timeStamp := prepareStore()

	ips := make([]*net.IP, 1)
	ip := net.ParseIP("3.3.3.3")
	ips[0] = &ip

	store.AddAlarm(expectedNamespace, ips)

	failed := store.GetFailed()

	if failed[expectedNamespace].FirstOccurance != timeStamp {
		t.Errorf("First occurance of failure is not valid. expected='%v', current='%v'",
			timeStamp,
			failed[expectedNamespace].FirstOccurance,
		)
	}

	if failed[expectedNamespace].LastOccurance == timeStamp {
		t.Errorf("Last occurance of failure is not valid. expected= junger than '%v', current='%v'",
			timeStamp,
			failed[expectedNamespace].LastOccurance,
		)
	}

	if !reflect.DeepEqual(failed[expectedNamespace].FailedIPs, ips) {
		t.Errorf("IPs do not match! expected='%v', current='%v'",
			failed[expectedNamespace].FailedIPs,
			ips,
		)
	}

	if failed[expectedNamespace].Counter != 2 {
		t.Errorf("The counter should be 2! expected='2', current='%v'", failed[expectedNamespace].Counter)
	}

	store.RemoveAlarm(expectedNamespace)
}

func TestAddTwoDifferentNamespaces(t *testing.T) {
	store, _ := prepareStore()

	ips := make([]*net.IP, 1)
	ip := net.ParseIP("3.3.3.3")
	ips[0] = &ip

	store.AddAlarm("other", ips)
	store.AddAlarm("other", ips)

	failed := store.GetFailed()

	if len(failed) != 2 {
		t.Errorf("Number of alarms don't match! expected=2, current='%v'", len(failed))
	}

	if failed[expectedNamespace].Counter != 1 {
		t.Errorf("The counter for failed alarm on namespace '%v' should be 1! expected=1, current=%v", expectedNamespace, failed[expectedNamespace].Counter)
	}

	if failed["other"].Counter != 2 {
		t.Errorf("The counter for failed alarm on namespace '%v' should be 2! expected=1, current=%v", "other", failed["other"].Counter)
	}

	store.RemoveAlarm("other")
	store.RemoveAlarm(expectedNamespace)
}
