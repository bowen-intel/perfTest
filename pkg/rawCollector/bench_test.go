package rawcollector

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

var pool sync.Pool
var addrPool sync.Pool
var data []byte

func init() {
	pool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 24+16*2, 24+16*2)
		},
	}
	addrPool = sync.Pool{
		New: func() interface{} {
			d := make([]byte, 24+16*2)
			return &d
		},
	}
	data = make([]byte, 24+16*2)
	for i := 0; i < 24+16*2; i++ {
		data[i] = byte(i)
	}
}

func Benchmark_Pool(b *testing.B) {
	for j := 0; j < 100; j++ {
		var wg sync.WaitGroup
		wg.Add(112 * 200)
		for i := 0; i < 22400; i++ {
			go func() {
				defer wg.Done()
				buf := pool.Get().([]byte)
				io.Copy(bytes.NewBuffer(buf), bytes.NewReader(data))
				pool.Put(buf)
			}()
		}
		wg.Wait()
	}
}

func Benchmark_AddrPool(b *testing.B) {
	for j := 0; j < 100; j++ {
		var wg sync.WaitGroup
		wg.Add(112 * 200)
		for i := 0; i < 22400; i++ {
			go func() {
				defer wg.Done()
				buf := addrPool.Get().(*([]byte))
				io.Copy(bytes.NewBuffer(*buf), bytes.NewReader(data))
				addrPool.Put(buf)
			}()
		}
		wg.Wait()
	}
}

func Benchmark_NoPool(b *testing.B) {
	for j := 0; j < 100; j++ {
		var wg sync.WaitGroup
		wg.Add(112 * 200)
		for i := 0; i < 22400; i++ {
			go func() {
				defer wg.Done()
				buf := make([]byte, 24+16*2)
				io.Copy(bytes.NewBuffer(buf), bytes.NewReader(data))
			}()
		}
		wg.Wait()
	}
}
