package guestagent

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/balaji113/macvz/pkg/socket"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/balaji113/macvz/pkg/guestagent/api"
	"github.com/balaji113/macvz/pkg/guestagent/iptables"
	"github.com/balaji113/macvz/pkg/guestagent/procnettcp"
	"github.com/balaji113/macvz/pkg/guestagent/timesync"
	"github.com/elastic/go-libaudit/v2"
	"github.com/elastic/go-libaudit/v2/auparse"
	"github.com/sirupsen/logrus"
	"github.com/yalue/native_endian"
)

func New(newTicker func() (<-chan time.Time, func()), vsock net.Conn, iptablesIdle time.Duration) (Agent, error) {
	a := &agent{
		newTicker: newTicker,
		vsock:     socket.VsockConnection{Conn: vsock},
	}

	auditClient, err := libaudit.NewMulticastAuditClient(nil)
	if err != nil {
		return nil, err
	}
	auditStatus, err := auditClient.GetStatus()
	if err != nil {
		return nil, err
	}
	if auditStatus.Enabled == 0 {
		if err = auditClient.SetEnabled(true, libaudit.WaitForReply); err != nil {
			return nil, err
		}
	}

	go a.setWorthCheckingIPTablesRoutine(auditClient, iptablesIdle)
	go a.fixSystemTimeSkew()
	return a, nil
}

type agent struct {
	// Ticker is like time.Ticker.
	// We can't use inotify for /proc/net/tcp, so we need this ticker to
	// reload /proc/net/tcp.
	newTicker func() (<-chan time.Time, func())
	vsock     socket.VsockConnection

	worthCheckingIPTables   bool
	worthCheckingIPTablesMu sync.RWMutex
	latestIPTables          []iptables.Entry
	latestIPTablesMu        sync.RWMutex
}

// setWorthCheckingIPTablesRoutine sets worthCheckingIPTables to be true
// when received NETFILTER_CFG audit message.
//
// setWorthCheckingIPTablesRoutine sets worthCheckingIPTables to be false
// when no NETFILTER_CFG audit message was received for the iptablesIdle time.
func (a *agent) setWorthCheckingIPTablesRoutine(auditClient *libaudit.AuditClient, iptablesIdle time.Duration) {
	var latestTrue time.Time
	go func() {
		for {
			time.Sleep(iptablesIdle)
			a.worthCheckingIPTablesMu.Lock()
			// time is monotonic, see https://pkg.go.dev/time#hdr-Monotonic_Clocks
			elapsedSinceLastTrue := time.Since(latestTrue)
			if elapsedSinceLastTrue >= iptablesIdle {
				logrus.Debug("setWorthCheckingIPTablesRoutine(): setting to false")
				a.worthCheckingIPTables = false
			}
			a.worthCheckingIPTablesMu.Unlock()
		}
	}()
	for {
		msg, err := auditClient.Receive(false)
		if err != nil {
			logrus.Error(err)
			continue
		}
		switch msg.Type {
		case auparse.AUDIT_NETFILTER_CFG:
			a.worthCheckingIPTablesMu.Lock()
			logrus.Debug("setWorthCheckingIPTablesRoutine(): setting to true")
			a.worthCheckingIPTables = true
			latestTrue = time.Now()
			a.worthCheckingIPTablesMu.Unlock()
		}
	}
}

type eventState struct {
	ports []api.IPPort
}

func comparePorts(old, neww []api.IPPort) (added, removed []api.IPPort) {
	mRaw := make(map[string]api.IPPort, len(old))
	mStillExist := make(map[string]bool, len(old))

	for _, f := range old {
		k := f.String()
		mRaw[k] = f
		mStillExist[k] = false
	}
	for _, f := range neww {
		k := f.String()
		if _, ok := mRaw[k]; !ok {
			added = append(added, f)
		}
		mStillExist[k] = true
	}

	for k, stillExist := range mStillExist {
		if !stillExist {
			if x, ok := mRaw[k]; ok {
				removed = append(removed, x)
			}
		}
	}
	return
}

func (a *agent) collectEvent(st eventState) (api.Event, eventState) {
	var (
		ev  api.Event
		err error
	)
	newSt := st
	newSt.ports, err = a.localPorts()
	if err != nil {
		ev.Errors = append(ev.Errors, err.Error())
		ev.Time = time.Now()
		return ev, newSt
	}
	ev.LocalPortsAdded, ev.LocalPortsRemoved = comparePorts(st.ports, newSt.ports)
	ev.Time = time.Now()
	return ev, newSt
}

func isEventEmpty(ev api.Event) bool {
	var empty api.Event
	// ignore ev.Time
	copied := ev
	copied.Time = time.Time{}
	return reflect.DeepEqual(empty, copied)
}

func (a *agent) ListenAndSendEvents() {
	tickerCh, tickerClose := a.newTicker()
	defer tickerClose()
	var st eventState
	for {
		var ev api.Event
		ev, st = a.collectEvent(st)
		if !isEventEmpty(ev) {
			marshal, err := json.Marshal(ev)
			if err != nil {
				logrus.Warn(err)
			}
			a.vsock.WriteEvents(string(marshal))
		}
		select {
		case _, ok := <-tickerCh:
			if !ok {
				return
			}
			logrus.Debug("tick!")
		}
	}
}

func (a *agent) localPorts() ([]api.IPPort, error) {
	if native_endian.NativeEndian() == binary.BigEndian {
		return nil, errors.New("big endian architecture is unsupported, because I don't know how /proc/net/tcp looks like on big endian hosts")
	}
	var res []api.IPPort
	tcpParsed, err := procnettcp.ParseFiles()
	if err != nil {
		return res, err
	}

	for _, f := range tcpParsed {
		switch f.Kind {
		case procnettcp.TCP, procnettcp.TCP6:
		default:
			continue
		}
		if f.State == procnettcp.TCPListen {
			res = append(res,
				api.IPPort{
					IP:   f.IP,
					Port: int(f.Port),
				})
		}
	}

	a.worthCheckingIPTablesMu.RLock()
	worthCheckingIPTables := a.worthCheckingIPTables
	a.worthCheckingIPTablesMu.RUnlock()
	logrus.Debugf("LocalPorts(): worthCheckingIPTables=%v", worthCheckingIPTables)

	var ipts []iptables.Entry
	if a.worthCheckingIPTables {
		ipts, err = iptables.GetPorts()
		if err != nil {
			return res, err
		}
		a.latestIPTablesMu.Lock()
		a.latestIPTables = ipts
		a.latestIPTablesMu.Unlock()
	} else {
		a.latestIPTablesMu.RLock()
		ipts = a.latestIPTables
		a.latestIPTablesMu.RUnlock()
	}

	for _, ipt := range ipts {
		// Make sure the port isn't already listed from procnettcp
		found := false
		for _, re := range res {
			if re.Port == ipt.Port {
				found = true
			}
		}
		if !found {
			res = append(res,
				api.IPPort{
					IP:   ipt.IP,
					Port: ipt.Port,
				})
		}
	}

	return res, nil
}

func (a *agent) PublishInfo() {
	var (
		info api.Info
		err  error
	)
	info.LocalPorts, err = a.localPorts()
	if err != nil {
		logrus.Error(err)
	}
	marshal, err := json.Marshal(info)
	if err != nil {
		logrus.Warn(err)
	}
	a.vsock.WriteEvents(string(marshal))
}

const deltaLimit = 2 * time.Second

func (a *agent) fixSystemTimeSkew() {
	for {
		ticker := time.NewTicker(10 * time.Second)
		for now := range ticker.C {
			rtc, err := timesync.GetRTCTime()
			if err != nil {
				logrus.Warnf("fixSystemTimeSkew: lookup error: %s", err.Error())
				continue
			}
			d := rtc.Sub(now)
			logrus.Debugf("fixSystemTimeSkew: rtc=%s systime=%s delta=%s",
				rtc.Format(time.RFC3339), now.Format(time.RFC3339), d)
			if d > deltaLimit || d < -deltaLimit {
				err = timesync.SetSystemTime(rtc)
				if err != nil {
					logrus.Warnf("fixSystemTimeSkew: set system clock error: %s", err.Error())
					continue
				}
				logrus.Infof("fixSystemTimeSkew: system time synchronized with rtc")
				break
			}
		}
		ticker.Stop()
	}
}
