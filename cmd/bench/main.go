package main

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	corecollector "github.com/Rouzip/goperf/pkg/coreCollector"
	rawcollector "github.com/Rouzip/goperf/pkg/rawCollector"
	"github.com/Rouzip/goperf/pkg/utils"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/util/wait"
)

// run collector for 100 containers
func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	ctx := utils.SetUpContext()
	go func() {
		<-ctx.Done()
		rawcollector.Finalize()
		wg.Done()
	}()

	cc := corecollector.NewCoreCollector(runtime.NumCPU())
	go wait.Until(cc.Profile, 60*time.Second, ctx.Done())
	// go wait.Until(func() {
	// 	// node name
	// 	pods, err := utils.GetTestPods(os.Args[1])
	// 	if err != nil {
	// 		klog.Fatal(err)
	// 	}
	// 	collector, err := pod.GeneratePodCollector("libpfm4", pods)
	// 	if err != nil {
	// 		klog.Fatal(err)
	// 	}
	// 	// collect perf CPI for 10s
	// 	time.Sleep(time.Second * 60)
	// 	// collector
	// 	collector.Profile()
	// }, 10*time.Second, ctx.Done())
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(":8080", nil)
	wg.Wait()
}
