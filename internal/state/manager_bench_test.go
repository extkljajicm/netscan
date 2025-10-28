package state

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkAddDevice tests the performance of adding devices
func BenchmarkAddDevice(b *testing.B) {
	benchmarks := []struct {
		name       string
		maxDevices int
		existing   int
	}{
		{"Small_100devices", 1000, 50},
		{"Medium_1Kdevices", 10000, 500},
		{"Large_10Kdevices", 20000, 5000},
		{"AtLimit_20Kdevices", 20000, 19999},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.maxDevices)

			// Pre-populate with existing devices
			for i := 0; i < bm.existing; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
				mgr.AddDevice(ip)
			}
		})
	}
}

// BenchmarkAddDeviceWithEviction tests performance when LRU eviction is triggered
func BenchmarkAddDeviceWithEviction(b *testing.B) {
	benchmarks := []struct {
		name       string
		maxDevices int
	}{
		{"Eviction_100devices", 100},
		{"Eviction_1Kdevices", 1000},
		{"Eviction_10Kdevices", 10000},
		{"Eviction_20Kdevices", 20000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.maxDevices)

			// Fill to capacity
			for i := 0; i < bm.maxDevices; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			// Every add now triggers eviction
			for i := 0; i < b.N; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
				mgr.AddDevice(ip)
			}
		})
	}
}

// BenchmarkGet tests the performance of retrieving devices
func BenchmarkGet(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
	}{
		{"Get_100devices", 100},
		{"Get_1Kdevices", 1000},
		{"Get_10Kdevices", 10000},
		{"Get_20Kdevices", 20000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ip := ips[i%len(ips)]
				_, _ = mgr.Get(ip)
			}
		})
	}
}

// BenchmarkGetAllIPs tests the performance of retrieving all IPs
func BenchmarkGetAllIPs(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
	}{
		{"GetAllIPs_100devices", 100},
		{"GetAllIPs_1Kdevices", 1000},
		{"GetAllIPs_10Kdevices", 10000},
		{"GetAllIPs_20Kdevices", 20000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = mgr.GetAllIPs()
			}
		})
	}
}

// BenchmarkUpdateLastSeen tests the performance of updating timestamps
func BenchmarkUpdateLastSeen(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
	}{
		{"UpdateLastSeen_100devices", 100},
		{"UpdateLastSeen_1Kdevices", 1000},
		{"UpdateLastSeen_10Kdevices", 10000},
		{"UpdateLastSeen_20Kdevices", 20000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				ip := ips[i%len(ips)]
				mgr.UpdateLastSeen(ip)
			}
		})
	}
}

// BenchmarkReportPingSuccess tests circuit breaker success reporting
func BenchmarkReportPingSuccess(b *testing.B) {
	mgr := NewManager(20000)

	// Populate with devices
	ips := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ips[i] = ip
		mgr.AddDevice(ip)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ip := ips[i%len(ips)]
		mgr.ReportPingSuccess(ip)
	}
}

// BenchmarkReportPingFail tests circuit breaker failure reporting
func BenchmarkReportPingFail(b *testing.B) {
	mgr := NewManager(20000)

	// Populate with devices
	ips := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ips[i] = ip
		mgr.AddDevice(ip)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ip := ips[i%len(ips)]
		mgr.ReportPingFail(ip, 10, 5*time.Minute)
	}
}

// BenchmarkIsSuspended tests circuit breaker suspension check
func BenchmarkIsSuspended(b *testing.B) {
	mgr := NewManager(20000)

	// Populate with devices
	ips := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		ips[i] = ip
		mgr.AddDevice(ip)
	}

	// Suspend some devices
	for i := 0; i < 100; i++ {
		for j := 0; j < 10; j++ {
			mgr.ReportPingFail(ips[i], 10, 5*time.Minute)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ip := ips[i%len(ips)]
		_ = mgr.IsSuspended(ip)
	}
}

// BenchmarkGetSuspendedCount tests counting suspended devices
func BenchmarkGetSuspendedCount(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
		suspended   int
	}{
		{"SuspendedCount_1K_100suspended", 1000, 100},
		{"SuspendedCount_10K_1Ksuspended", 10000, 1000},
		{"SuspendedCount_20K_2Ksuspended", 20000, 2000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			// Suspend some devices
			for i := 0; i < bm.suspended; i++ {
				for j := 0; j < 10; j++ {
					mgr.ReportPingFail(ips[i], 10, 5*time.Minute)
				}
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = mgr.GetSuspendedCount()
			}
		})
	}
}

// BenchmarkPruneStale tests pruning performance
func BenchmarkPruneStale(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
		staleCount  int
	}{
		{"Prune_1K_100stale", 1000, 100},
		{"Prune_10K_1Kstale", 10000, 1000},
		{"Prune_20K_2Kstale", 20000, 2000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				mgr := NewManager(bm.deviceCount * 2)

				// Populate with devices
				for j := 0; j < bm.deviceCount; j++ {
					ip := fmt.Sprintf("192.168.%d.%d", j/256, j%256)
					dev := Device{
						IP:       ip,
						Hostname: ip,
					}
					// Make some devices stale
					if j < bm.staleCount {
						dev.LastSeen = time.Now().Add(-25 * time.Hour)
					} else {
						dev.LastSeen = time.Now()
					}
					mgr.Add(dev)
				}

				b.StartTimer()
				_ = mgr.PruneStale(24 * time.Hour)
			}
		})
	}
}

// BenchmarkConcurrentReads tests concurrent read performance
func BenchmarkConcurrentReads(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
		goroutines int
	}{
		{"ConcurrentReads_1K_10goroutines", 1000, 10},
		{"ConcurrentReads_10K_100goroutines", 10000, 100},
		{"ConcurrentReads_20K_1000goroutines", 20000, 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			var wg sync.WaitGroup
			perGoroutine := b.N / bm.goroutines

			for g := 0; g < bm.goroutines; g++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for i := 0; i < perGoroutine; i++ {
						ip := ips[(goroutineID*perGoroutine+i)%len(ips)]
						_, _ = mgr.Get(ip)
					}
				}(g)
			}

			wg.Wait()
		})
	}
}

// BenchmarkConcurrentWrites tests concurrent write performance
func BenchmarkConcurrentWrites(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
		goroutines int
	}{
		{"ConcurrentWrites_1K_10goroutines", 1000, 10},
		{"ConcurrentWrites_10K_100goroutines", 10000, 100},
		{"ConcurrentWrites_20K_1000goroutines", 20000, 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			var wg sync.WaitGroup
			perGoroutine := b.N / bm.goroutines

			for g := 0; g < bm.goroutines; g++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for i := 0; i < perGoroutine; i++ {
						ip := ips[(goroutineID*perGoroutine+i)%len(ips)]
						mgr.UpdateLastSeen(ip)
					}
				}(g)
			}

			wg.Wait()
		})
	}
}

// BenchmarkConcurrentMixed tests mixed read/write performance
func BenchmarkConcurrentMixed(b *testing.B) {
	benchmarks := []struct {
		name       string
		deviceCount int
		goroutines int
	}{
		{"ConcurrentMixed_1K_10goroutines", 1000, 10},
		{"ConcurrentMixed_10K_100goroutines", 10000, 100},
		{"ConcurrentMixed_20K_1000goroutines", 20000, 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			mgr := NewManager(bm.deviceCount * 2)

			// Populate with devices
			ips := make([]string, bm.deviceCount)
			for i := 0; i < bm.deviceCount; i++ {
				ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
				ips[i] = ip
				mgr.AddDevice(ip)
			}

			b.ResetTimer()

			var wg sync.WaitGroup
			perGoroutine := b.N / bm.goroutines

			for g := 0; g < bm.goroutines; g++ {
				wg.Add(1)
				go func(goroutineID int) {
					defer wg.Done()
					for i := 0; i < perGoroutine; i++ {
						ip := ips[(goroutineID*perGoroutine+i)%len(ips)]
						if i%2 == 0 {
							_, _ = mgr.Get(ip) // Read
						} else {
							mgr.UpdateLastSeen(ip) // Write
						}
					}
				}(g)
			}

			wg.Wait()
		})
	}
}
