package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
)

const (
	Cycles       = "cylces"
	Instructions = "instructions"
	Namespace    = "namespace"
	Pod          = "pod"
	Container    = "container"
	ContainerID  = "containerid"
	CPIFieid     = "cpi_type"
)

var (
	ContainerCPI = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "container_cpi",
	}, []string{Namespace, Pod, Container, ContainerID, CPIFieid})
	CPICollectors = []prometheus.Collector{ContainerCPI}
	CoreMPKI      = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "core_mpki",
	}, []string{"node", "core", "mpki_type"})
	MPKICollectors = []prometheus.Collector{CoreMPKI}
)

func init() {
	prometheus.MustRegister(CPICollectors...)
	prometheus.MustRegister(MPKICollectors...)
}

func RecordMPKI(node, perfType string, cpu int, value float64) {
	labels := prometheus.Labels{}
	labels["node"] = node
	labels["mpki_type"] = perfType
	labels["core"] = fmt.Sprint(cpu)
	CoreMPKI.With(labels).Set(value)
}

func RecordCPI(container *v1.ContainerStatus, pod *v1.Pod, cycles, ins float64) {
	labels := prometheus.Labels{}
	labels[Namespace] = pod.Namespace
	labels[Pod] = pod.Name
	labels[Container] = container.Name
	labels[ContainerID] = container.ContainerID
	labels[CPIFieid] = Cycles
	ContainerCPI.With(labels).Set(cycles)

	labels[CPIFieid] = Instructions
	ContainerCPI.With(labels).Set(ins)
}
