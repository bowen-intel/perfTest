package pod

import (
	"fmt"
	"sync"

	gorawcollector "github.com/Rouzip/goperf/pkg/goRawCollector"
	"github.com/Rouzip/goperf/pkg/metrics"
	rawcollector "github.com/Rouzip/goperf/pkg/rawCollector"
	"github.com/Rouzip/goperf/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type PodCollector struct {
	PodCollectorMap sync.Map
	UnitMap         map[utils.Unit]struct{}
	Type            string
}

func GeneratePodCollector(t string, pods []*v1.Pod) (*PodCollector, error) {
	collector := &PodCollector{
		PodCollectorMap: sync.Map{},
		UnitMap:         make(map[utils.Unit]struct{}),
	}

	for _, pod := range pods {
		for _, container := range pod.Status.ContainerStatuses {
			collector.UnitMap[utils.Unit{
				Container: container.Name,
				Pod:       pod.Name,
				Namespace: pod.Namespace,
			}] = struct{}{}
			if t == "goraw" {
				go func(pod *v1.Pod, container *v1.ContainerStatus) {
					collector.PodCollectorMap.Store(utils.Unit{
						Container: container.Name,
						Pod:       pod.Name,
						Namespace: pod.Namespace,
					}, gorawcollector.NewGoRawCollector(pod, container))
				}(pod, &container)
			} else if t == "libpfm4" {
				go func(pod *v1.Pod, container *v1.ContainerStatus) {
					collector.PodCollectorMap.Store(utils.Unit{
						Container: container.Name,
						Pod:       pod.Name,
						Namespace: pod.Namespace,
					}, rawcollector.NewRawCollector(pod, container))
				}(pod, &container)
			} else {
				return nil, fmt.Errorf("unknown collector type: %s", t)
			}
		}
	}
	collector.Type = t

	return collector, nil
}

func (p *PodCollector) Profile() {
	// go util end
	var wg sync.WaitGroup
	wg.Add(len(p.UnitMap))

	for unit := range p.UnitMap {
		go func(unit utils.Unit) {
			klog.Info(unit)
			defer wg.Done()
			if collector, ok := p.PodCollectorMap.Load(unit); ok {
				if p.Type == "goraw" {
					goc := collector.(*gorawcollector.GoRawCollector)
					goc.Collect()
					metrics.RecordCPI(goc.Container, goc.Pod, float64(goc.Cycle), float64(goc.Instruction))
					defer goc.Close()
				} else if p.Type == "libpfm4" {

				} else {
					klog.Fatal("unknown collector type")
				}
			}
		}(unit)
	}

	wg.Wait()
}

func (p *PodCollector) Close() error {
	return nil
}
