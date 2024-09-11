package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

var (
	mode = flag.String("mode", "", "internal mode, don't touch")

	target      = flag.String("target", "http://23.176.168.2:9000", "target URL")
	n           = flag.Int("n", 1000, "Number of sequential http requests per goroutine")
	concurrency = flag.Int("concurrency", 30, "Number of concurrent goroutines to run")
	routines    = flag.Int("routines", 100, "total number of goroutines to start")

	subnet = flag.String("subnet", "", "subnet for client IPs")
)

var (
	mu             sync.Mutex
	totalRequests  int
	failedRequests int
)

func run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	sout := string(out)
	if err != nil {
		return sout, err
	}
	return sout, nil
}

func runOrDie(ctx context.Context, args ...string) string {
	out, err := run(ctx, args...)
	if err != nil {
		log.Fatalf("run %q: %s: %s\n", args, err, string(out))
	}
	return out
}

func runNSOrDie(ctx context.Context, ns string, args ...string) string {
	return runOrDie(ctx, slices.Concat([]string{"ip", "netns", "exec", ns}, args)...)
}

func sendRequests() {
	lastLocalAddr := ""
	transport := http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network string, addr string) (conn net.Conn, err error) {
			port := 50000 + rand.Int31n(15000)
			localAddr, err := net.ResolveTCPAddr("tcp4", fmt.Sprintf(":%d", port))
			if err != nil {
				return nil, err
			}
			lastLocalAddr = localAddr.String()
			d := &net.Dialer{
				LocalAddr: localAddr,
			}
			return d.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	c := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &transport,
	}
	for range *n {
		mu.Lock()
		totalRequests++
		mu.Unlock()
		_, err := c.Get(*target)
		if err != nil && strings.Contains(err.Error(), "already in use") {
			continue
		}
		if err != nil {
			mu.Lock()
			failedRequests++
			mu.Unlock()
			log.Printf("failed to fetch url %s from %s: %s\n", *target, lastLocalAddr, err)
		}
		c.CloseIdleConnections()
		time.Sleep(25 * time.Millisecond)
	}
}

type logWriter struct {
	prefix string
}

func (l logWriter) Write(p []byte) (n int, err error) {
	log.Printf("%s%s", l.prefix, string(p))
	return len(p), nil
}

func main() {
	flag.Parse()

	if *mode == "requests" {
		sendRequests()
		mu.Lock()
		log.Printf("worker requests sent %d, failed requests %d", totalRequests, failedRequests)
		mu.Unlock()
		return
	}

	_, ipnet, err := net.ParseCIDR(*subnet)
	if err != nil {
		log.Fatalf("failed to parse CIDR: %s", err)
	}
	ones, _ := ipnet.Mask.Size()
	// For simplicity.
	if ones != 16 {
		log.Fatalf("--subnet should be a /16")
	}

	log.Printf("Starting test, this may take a few minutes...")

	ctx := context.Background()

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(*concurrency)

	//go func() {
	//	for {
	//		select {
	//		case <-egCtx.Done():
	//			return
	//		case <-time.After(5 * time.Second):
	//		}
	//		mu.Lock()
	//		log.Printf("requests sent %d, failed requests %d", totalRequests, failedRequests)
	//		mu.Unlock()
	//
	//	}
	//}()

	ip := ipnet.IP

	for i := range *routines {
		i := i
		ip[3]++
		nsMaskedIP := ip.String() + "/30"
		ip[3]++
		hostMaskedIP := ip.String() + "/30"
		hostIP := ip.String()

		if ip[3] == 254 {
			ip[2]++
			ip[3] = 0
		} else {
			ip[3] += 2
		}

		ns := fmt.Sprintf("repro_%d", i)
		hostIntf := fmt.Sprintf("vethhostrepro%d", i)
		out, err := run(ctx, "ip", "netns", "add", ns)
		if err != nil && !strings.Contains(out, "File exists") {
			log.Fatalf("cmd failed: %s", err)
		}
		if err == nil {
			runNSOrDie(ctx, ns, "ip", "link", "add", hostIntf, "type", "veth", "peer", "name", "veth0")
			runNSOrDie(ctx, ns, "ip", "link", "set", hostIntf, "netns", "1")
			runNSOrDie(ctx, ns, "ip", "addr", "add", nsMaskedIP, "dev", "veth0")
			runOrDie(ctx, "ip", "addr", "add", hostMaskedIP, "dev", hostIntf)
			runOrDie(ctx, "ip", "link", "set", hostIntf, "up")
			runNSOrDie(ctx, ns, "ip", "link", "set", "veth0", "up")
			runNSOrDie(ctx, ns, "ip", "route", "add", "default", "via", hostIP)
		}
		ipCopy := make(net.IP, len(ipnet.IP))
		copy(ipCopy, ipnet.IP)
		eg.Go(func() error {
			cmd := exec.CommandContext(egCtx, "ip", "netns", "exec", ns, "./repro", "--mode", "requests")
			l := &logWriter{prefix: fmt.Sprintf("[worker %d] ", i)}
			cmd.Stdout = l
			cmd.Stderr = l
			err := cmd.Run()
			if err != nil {
				log.Fatalf("could not run binary in request mode: %s", err)
			}
			log.Printf("worker %d finished", i)
			return nil
		})
	}

	_ = eg.Wait()
}
