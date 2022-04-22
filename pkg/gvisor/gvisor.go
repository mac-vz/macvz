package proxy

import (
	"bufio"
	"context"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/mac-vz/macvz/pkg/socket"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/containers/gvisor-tap-vsock/pkg/sshclient"
	"github.com/containers/gvisor-tap-vsock/pkg/transport"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

var (
	debug           bool
	mtu             int
	endpoints       arrayFlags
	qemuSocket      string
	forwardSocket   arrayFlags
	forwardDest     arrayFlags
	forwardUser     arrayFlags
	forwardIdentify arrayFlags
	pidFile         string
	exitCode        int
)

const (
	gatewayIP   = "192.168.127.1"
	sshHostPort = "192.168.127.2:22"
)

func StartProxy(gVisorSock string, listenDebug bool, vzGVisorSock string, macAddr string, vzDataGram *net.UnixConn, addr net.Addr) {
	endpoints.Set("unix://" + gVisorSock)
	debug = listenDebug
	qemuSocket = "unix://" + vzGVisorSock
	mtu = 1500
	pidFile = ""
	ctx, cancel := context.WithCancel(context.Background())

	groupErrs, ctx := errgroup.WithContext(ctx)

	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// Make sure the qemu socket provided is valid syntax
	if len(qemuSocket) > 0 {
		uri, err := url.Parse(qemuSocket)
		if err != nil || uri == nil {
			exitWithError(errors.Wrapf(err, "invalid value for listen-qemu"))
		}
		if _, err := os.Stat(uri.Path); err == nil && uri.Scheme == "unix" {
			exitWithError(errors.Errorf("%q already exists", uri.Path))
		}
	}

	protocol := types.BessProtocol

	if c := len(forwardSocket); c != len(forwardDest) || c != len(forwardUser) || c != len(forwardIdentify) {
		exitWithError(errors.New("-forward-sock, --forward-dest, --forward-user, and --forward-identity must all be specified together, " +
			"the same number of times, or not at all"))
	}

	for i := 0; i < len(forwardSocket); i++ {
		_, err := os.Stat(forwardIdentify[i])
		if err != nil {
			exitWithError(errors.Wrapf(err, "Identity file %s can't be loaded", forwardIdentify[i]))
		}
	}

	// Create a PID file if requested
	if len(pidFile) > 0 {
		f, err := os.Create(pidFile)
		if err != nil {
			exitWithError(err)
		}
		// Remove the pid-file when exiting
		defer func() {
			if err := os.Remove(pidFile); err != nil {
				logrus.Error(err)
			}
		}()
		pid := os.Getpid()
		if _, err := f.WriteString(strconv.Itoa(pid)); err != nil {
			exitWithError(err)
		}
	}

	config := types.Configuration{
		Debug:             debug,
		CaptureFile:       captureFile(),
		MTU:               mtu,
		Subnet:            "192.168.127.0/24",
		GatewayIP:         gatewayIP,
		GatewayMacAddress: "5a:94:ef:e4:0c:dd",
		DHCPStaticLeases: map[string]string{
			"192.168.127.2": macAddr,
		},
		Forwards: map[string]string{
			fmt.Sprintf("127.0.0.1:%d", 2223): sshHostPort,
		},
		DNS: []types.Zone{
			{
				Name: "docker.internal.",
				Records: []types.Record{
					{
						Name: "gateway",
						IP:   net.ParseIP(gatewayIP),
					},
					{
						Name: "host",
						IP:   net.ParseIP("192.168.127.254"),
					},
				},
			},
		},
		DNSSearchDomains: searchDomains(),
		NAT: map[string]string{
			"192.168.127.254": "127.0.0.1",
		},
		GatewayVirtualIPs: []string{"192.168.127.254"},
		Protocol:          protocol,
	}

	groupErrs.Go(func() error {
		err := run(ctx, groupErrs, &config, endpoints)
		if err != nil {
			return err
		}
		time.Sleep(time.Second * 5)
		conn, err2 := net.Dial("unix", vzGVisorSock)
		if err2 != nil {
			logrus.Fatal("Error while listening to gvisor", err2)
		}
		socket.Pipe(vzDataGram, conn, addr)
		return nil
	})

	// Wait for something to happen
	groupErrs.Go(func() error {
		select {
		// Catch signals so exits are graceful and defers can run
		//case <-errCh:
		//	logrus.Infof("Stopping all")
		//	cancel()
		//	return errors.New("signal caught")
		case <-ctx.Done():
			logrus.Infof("Context done")
			cancel()
			return nil
		}
	})
	//if err := groupErrs.Wait(); err != nil {
	//	logrus.Error(err)
	//	exitCode = 1
	//}
}

type arrayFlags []string

func (i *arrayFlags) String() string {
	return "my string representation"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func captureFile() string {
	if !debug {
		return ""
	}
	return "capture.pcap"
}

func run(ctx context.Context, g *errgroup.Group, configuration *types.Configuration, endpoints []string) error {
	vn, err := virtualnetwork.New(configuration)
	if err != nil {
		return err
	}
	logrus.Info("waiting for clients...")

	for _, endpoint := range endpoints {
		logrus.Infof("listening %s", endpoint)
		ln, err := transport.Listen(endpoint)
		if err != nil {
			return errors.Wrap(err, "cannot listen")
		}
		httpServe(ctx, g, ln, withProfiler(vn))
	}

	ln, err := vn.Listen("tcp", fmt.Sprintf("%s:80", gatewayIP))
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/services/forwarder/all", vn.Mux())
	mux.Handle("/services/forwarder/expose", vn.Mux())
	mux.Handle("/services/forwarder/unexpose", vn.Mux())
	httpServe(ctx, g, ln, mux)

	if debug {
		g.Go(func() error {
		debugLog:
			for {
				select {
				case <-time.After(5 * time.Second):
					fmt.Printf("%v sent to the VM, %v received from the VM\n", humanize.Bytes(vn.BytesSent()), humanize.Bytes(vn.BytesReceived()))
				case <-ctx.Done():
					break debugLog
				}
			}
			return nil
		})
	}

	if qemuSocket != "" {
		qemuListener, err := transport.Listen(qemuSocket)
		if err != nil {
			return err
		}

		g.Go(func() error {
			<-ctx.Done()
			if err := qemuListener.Close(); err != nil {
				logrus.Errorf("error closing %s: %q", qemuSocket, err)
			}
			return os.Remove(qemuSocket)
		})

		g.Go(func() error {
			conn, err := qemuListener.Accept()
			if err != nil {
				return errors.Wrap(err, "qemu accept error")

			}
			return vn.AcceptQemu(ctx, conn)
		})
	}

	for i := 0; i < len(forwardSocket); i++ {
		var (
			src *url.URL
			err error
		)
		if strings.Contains(forwardSocket[i], "://") {
			src, err = url.Parse(forwardSocket[i])
			if err != nil {
				return err
			}
		} else {
			src = &url.URL{
				Scheme: "unix",
				Path:   forwardSocket[i],
			}
		}

		dest := &url.URL{
			Scheme: "ssh",
			User:   url.User(forwardUser[i]),
			Host:   sshHostPort,
			Path:   forwardDest[i],
		}
		j := i
		g.Go(func() error {
			defer os.Remove(forwardSocket[j])
			forward, err := sshclient.CreateSSHForward(ctx, src, dest, forwardIdentify[j], vn)
			if err != nil {
				return err
			}
			go func() {
				<-ctx.Done()
				// Abort pending accepts
				forward.Close()
			}()
		loop:
			for {
				select {
				case <-ctx.Done():
					break loop
				default:
					// proceed
				}
				err := forward.AcceptAndTunnel(ctx)
				if err != nil {
					logrus.Debugf("Error occurred handling ssh forwarded connection: %q", err)
				}
			}
			return nil
		})
	}

	return nil
}

func httpServe(ctx context.Context, g *errgroup.Group, ln net.Listener, mux http.Handler) {
	g.Go(func() error {
		<-ctx.Done()
		return ln.Close()
	})
	g.Go(func() error {
		err := http.Serve(ln, mux)
		if err != nil {
			if err != http.ErrServerClosed {
				return err
			}
			return err
		}
		return nil
	})
}

func withProfiler(vn *virtualnetwork.VirtualNetwork) http.Handler {
	mux := vn.Mux()
	if debug {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	}
	return mux
}

func exitWithError(err error) {
	logrus.Infof("Data error", err)
	logrus.Error(err)
	os.Exit(1)
}

func searchDomains() []string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		f, err := os.Open("/etc/resolv.conf")
		if err != nil {
			logrus.Errorf("open file error: %v", err)
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		searchPrefix := "search "
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), searchPrefix) {
				searchDomains := strings.Split(strings.TrimPrefix(sc.Text(), searchPrefix), " ")
				logrus.Debugf("Using search domains: %v", searchDomains)
				return searchDomains
			}
		}
		if err := sc.Err(); err != nil {
			logrus.Errorf("scan file error: %v", err)
			return nil
		}
	}
	return nil
}
