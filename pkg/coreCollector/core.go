package corecollector

import (
	"sync"
	"time"

	rawcollector "github.com/Rouzip/goperf/pkg/rawCollector"
)

type CoreCollector struct {
	coreNum          int
	coreCollectorMap map[int]*rawcollector.PerfCoreCollector
}

func NewCoreCollector(coreNum int) *CoreCollector {
	ccm := &CoreCollector{
		coreNum:          coreNum,
		coreCollectorMap: make(map[int]*rawcollector.PerfCoreCollector),
	}
	rawcollector.InitCoreEvent([]string{"MEM_LOAD_RETIRED.L3_MISS", "INST_RETIRED.ANY"})
	rawcollector.InitMPKIAttr()
	for i := 0; i < coreNum; i++ {
		pcc, err := rawcollector.NewPerfCoreCollector(i, []string{"MEM_LOAD_RETIRED.L3_MISS", "INST_RETIRED.ANY"})
		if err != nil {
			panic(err)
		}
		ccm.coreCollectorMap[i] = pcc
	}
	return ccm
}

func (ccm *CoreCollector) Profile() {
	var wg sync.WaitGroup
	wg.Add(ccm.coreNum)
	for _, pcc := range ccm.coreCollectorMap {
		go func(pcc *rawcollector.PerfCoreCollector) {
			pcc.Enable()
			time.Sleep(time.Second * 60)
			pcc.Collect()
			defer wg.Done()
		}(pcc)
	}
	wg.Wait()
}
