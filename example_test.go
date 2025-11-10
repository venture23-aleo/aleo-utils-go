package aleo_utils_test

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	aleo "github.com/venture23-aleo/aleo-utils-go"
)

func TestMonitorRSS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("rss monitor: skipping, /proc is unavailable on this platform")
	}

	log.Println("Process id:", os.Getpid())

	wrapper, closeFn, err := aleo.NewWrapper()
	if err != nil {
		t.Fatalf("create wrapper: %v", err)
	}
	defer closeFn()

	s, err := wrapper.NewSession()
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	privKey, address, err := s.NewPrivateKey()
	if err != nil {
		t.Fatalf("create private key: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				stats, err := readProcessRSS()
				if err != nil {
					t.Logf("rss monitor: error reading VmRSS: %v", err)
					continue
				}
				t.Logf("[%s] VmRSS=%d kB  VmSize=%d kB  VmHWM=%d kB  Threads=%d\n",
			time.Now().Format("15:04:05"), stats.VmRSS, stats.VmSize, stats.VmHWM, stats.Threads)
			case <-ctx.Done():
				return
			}
		}
	}()

	const iterations = 5000
	for i := 0; i < iterations; i++ {
		formattedMessage, err := s.FormatMessage([]byte("btc/usd = 1.0"), 1)
		if err != nil {
			t.Fatalf("format message: %v", err)
		}

		hashedMessage, err := s.HashMessage(formattedMessage)
		if err != nil {
			t.Fatalf("hash message: %v", err)
		}

		_, err = s.Sign(privKey, hashedMessage)
		if err != nil {
			t.Fatalf("sign message: %v", err)
		}

		if (i+1)%50 == 0 {
			t.Logf("rss monitor: completed %d/%d signing iterations", i+1, iterations)
		}

		// add delay to simulate real-world usage
		// time.Sleep(5 * time.Second)
	}

	cancel()
	wg.Wait()

	t.Log("rss monitor: final address:", address)
}

type ProcStats struct {
	VmRSS   int
	VmHWM   int
	VmSize  int
	Threads int
}

func readProcessRSS() (ProcStats, error) {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return ProcStats{}, err
	}
	defer f.Close()

	stats := ProcStats{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			fmt.Sscanf(line, "VmRSS: %d kB", &stats.VmRSS)
		case strings.HasPrefix(line, "VmHWM:"):
			fmt.Sscanf(line, "VmHWM: %d kB", &stats.VmHWM)
		case strings.HasPrefix(line, "VmSize:"):
			fmt.Sscanf(line, "VmSize: %d kB", &stats.VmSize)
		case strings.HasPrefix(line, "Threads:"):
			fmt.Sscanf(line, "Threads: %d", &stats.Threads)
		}
	}

	return stats, scanner.Err()
}
