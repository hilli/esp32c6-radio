//go:build tinygo && esp32c6 && esp32c6_radio

// Isolated, operator-scoped Thread campaign. No credentials or writes at boot.
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"time"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	threadnode "github.com/hilli/esp32c6-radio/examples/thread-node"
	"github.com/hilli/esp32c6-radio/lab"
	"github.com/hilli/esp32c6-radio/settings"
	"github.com/hilli/gothread/instance"
	"github.com/hilli/gothread/meshcop"
	"github.com/hilli/gothread/net/icmp6"
	"github.com/hilli/gothread/net/ip6"
	"github.com/hilli/gothread/thread/mle"
	"github.com/hilli/gothread/utils"
)

var firmwareID = "UNSET"

type echoes struct{ in, out uint64 }

func (e *echoes) HandleReply(utils.PingReply)   { e.out++ }
func (*echoes) HandleDone(utils.PingStatistics) {}

func emit(s *lab.Session, e lab.Event) {
	p, err := s.Record(e)
	if err != nil {
		println("Thread evidence encoding failed")
		return
	}
	fmt.Fprint(os.Stdout, string(p))
}
func onLoop(inst *instance.Instance, fn func() error) error {
	done := make(chan error, 1)
	inst.PostTask(func() { done <- fn() })
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return fmt.Errorf("Thread loop observation timed out")
	}
}
func monitor(inst *instance.Instance, driver *esp32c6radio.Radio, store *settings.Store, c lab.Command, s *lab.Session) {
	observed := &echoes{}
	peer, err := netip.ParseAddr(c.PeerIPv6)
	if err != nil || !peer.Is6() {
		emit(s, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: "explicit isolated IPv6 peer required"})
		return
	}
	err = onLoop(inst, func() error {
		return inst.IcmpManager().RegisterHandler(icmp6.NewHandler(func(_ *ip6.MessageInfo, h *icmp6.Header) {
			if h.GetType() == icmp6.TypeEchoRequest {
				observed.in++
			}
		}))
	})
	if err != nil {
		emit(s, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: err.Error()})
		return
	}
	begin := time.Now()
	attachDeadline := begin.Add(90 * time.Second)
	var attached time.Time
	var detached uint64
	var baseline, maxHeap uint64
	var final map[string]uint64
	for {
		var child bool
		var mac, mleCounter uint32
		var echoIn, echoOut uint64
		err = onLoop(inst, func() error {
			child = inst.MleService().Mle().Role() == mle.RoleChild
			mac, mleCounter = inst.GetMacFrameCounter(), inst.GetMleFrameCounter()
			echoIn, echoOut = observed.in, observed.out
			if child && !inst.PingSender().IsActive() {
				if err := inst.PingSender().Start(utils.PingConfig{Destination: peer.As16(), Count: 3, Interval: 1000, Timeout: 3000}, observed); err != nil {
					return err
				}
			}
			return inst.PersistenceError()
		})
		if err != nil {
			emit(s, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: err.Error()})
			return
		}
		if !child && attached.IsZero() && time.Now().After(attachDeadline) {
			emit(s, lab.Event{Probe: "thread", Stage: "attach", Status: lab.Fail, Reason: "missing peer/channel mismatch/attach timeout"})
			return
		}
		if child && attached.IsZero() {
			attached = time.Now()
			runtime.GC()
		}
		if !child && !attached.IsZero() {
			detached++
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		used := memory.HeapAlloc
		maxHeap = max(maxHeap, used)
		if !attached.IsZero() && time.Since(attached) >= 30*time.Second && baseline == 0 {
			runtime.GC()
			runtime.ReadMemStats(&memory)
			baseline = memory.HeapAlloc
		}
		stats := driver.Statistics()
		storage := store.Statistics()
		final = map[string]uint64{"role_child": 0, "secured_tx": stats.SecuredTransmitAttempts, "echo_in": echoIn, "echo_out": echoOut,
			"mac_counter": uint64(mac), "mle_counter": uint64(mleCounter), "heap_bytes": used, "heap_high_water": maxHeap, "rx_overflow": stats.ReceiveDropped,
			"flash_program_bytes": storage.ProgramBytes, "flash_erase_sectors": storage.EraseSectors, "radio_pause_microseconds": stats.FlashPauseMicroseconds,
			"reboot_counter_checks": 0, "peer_echo_artifact": 0}
		final["detached_samples"] = detached
		if child {
			final["role_child"] = 1
		}
		emit(s, lab.Event{Probe: "thread", Stage: "observation", Status: lab.Pass, Generation: storage.Generation, Metrics: final, Reason: "measurements only; peer and reboot witness required"})
		if !attached.IsZero() && time.Since(attached) >= time.Duration(c.DurationSeconds)*time.Second {
			break
		}
		time.Sleep(time.Second)
	}
	status := lab.Pass
	if final["role_child"] != 1 || final["secured_tx"] == 0 || final["echo_in"] == 0 || final["echo_out"] == 0 || detached != 0 {
		status = lab.Fail
	}
	emit(s, lab.Event{Probe: "thread", Stage: "summary", Status: status, Metrics: final, Reason: "device observations; independent peer replies and reboot/counter reconciliation still required"})
	runtime.GC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	duration := uint64(time.Since(attached).Seconds())
	soak := lab.NotRun
	metrics := map[string]uint64{"duration_seconds": duration, "heap_start": baseline, "heap_end": memory.HeapAlloc, "heap_high_water": maxHeap, "bounded_memory": 0}
	if duration >= 1800 {
		soak = lab.Fail
		if baseline > 0 && memory.HeapAlloc <= baseline+32768 && status == lab.Pass {
			soak = lab.Pass
			metrics["bounded_memory"] = 1
		}
	}
	emit(s, lab.Event{Probe: "thread", Stage: "soak", Status: soak, Metrics: metrics, Reason: "explicit 32KiB retained-heap growth limit after warmup; not a certification"})
}
func main() {
	driver, err := esp32c6radio.New()
	if err != nil {
		println(err.Error())
		select {}
	}
	eui := driver.GetIEEEEui64()
	session := lab.Session{FirmwareID: firmwareID, DeviceEUI: hex.EncodeToString(eui[:])}
	s := bufio.NewScanner(lab.SerialReader{})
	s.Buffer(make([]byte, 2048), 2048)
	for s.Scan() {
		c, err := lab.ParseCommand(s.Bytes())
		if err != nil {
			println("invalid Thread command (input is never echoed)")
			continue
		}
		if session.Arm(c) != nil {
			println("Thread identity not armed")
			continue
		}
		if c.Operation == "hello" {
			emit(&session, lab.Event{Probe: "thread", Stage: "hello", Status: lab.Pass, Reason: "no credentials or flash writes at boot"})
			continue
		}
		if c.Operation != "start" || c.Confirmation != session.DeviceEUI+":thread-lab:"+session.RunID || len(c.SourceReviewArtifact) != 64 || len(c.StorageReviewArtifact) != 64 || len(c.EntropyReviewArtifact) != 64 || c.DurationSeconds < 60 || c.DurationSeconds > 3600 {
			emit(&session, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: "explicit disposable-network scope, source/storage review artifacts and bounded duration required"})
			continue
		}
		var info *meshcop.Info
		if c.DatasetHex != "" {
			p, err := hex.DecodeString(c.DatasetHex)
			if err != nil || len(p) > meshcop.DatasetMaxLength {
				emit(&session, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: "invalid private dataset input"})
				continue
			}
			var dataset meshcop.Dataset
			if dataset.SetFrom(p) != nil || !dataset.ContainsAllRequiredTLVsFor(meshcop.DatasetActive) {
				clear(p)
				emit(&session, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: "complete isolated active dataset required"})
				continue
			}
			converted := dataset.ConvertToInfo()
			info = &converted
			clear(p)
		}
		c.DatasetHex = "" // never put private dataset contents in evidence
		store, err := settings.Open(driver)
		if err != nil {
			emit(&session, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: err.Error()})
			continue
		}
		stop := make(chan struct{})
		err = threadnode.RunWithDriver(driver, store, info, stop, func(inst *instance.Instance) {
			monitor(inst, driver, store, c, &session)
			close(stop)
		})
		if err != nil {
			emit(&session, lab.Event{Probe: "thread", Stage: "summary", Status: lab.Fail, Reason: err.Error()})
		}
		if err := store.Close(); err != nil {
			println("store close failed")
		}
		// Closed radio/Instance ownership is terminal. A new boot is required
		// for the next recovery observation, never a resettable RAM counter.
		select {}
	}
	if err := s.Err(); err != nil {
		println(err.Error())
	}
	select {}
}
