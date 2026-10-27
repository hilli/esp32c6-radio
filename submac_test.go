package esp32c6radio

import (
	"testing"
	"time"

	"github.com/hilli/gothread/mac/submac"
	"github.com/hilli/gothread/radio"
)

type macResults struct {
	completed int
	last      error
	cca       []bool
}

func (*macResults) ReceiveDone(*radio.Frame, error) {}
func (r *macResults) TransmitDone(_ *radio.Frame, _ *radio.Frame, err error) {
	r.completed++
	r.last = err
}
func (*macResults) RecordFrameTransmitStatus(*radio.Frame, error, uint8, bool) {}
func (r *macResults) RecordCcaStatus(ok bool, _ uint8)                         { r.cca = append(r.cca, ok) }
func (*macResults) EnergyScanDone(int8)                                        {}
func (*macResults) FrameCounterUsed(uint32)                                    {}

func TestRealSubMacSoftwareBackoffAndRetries(t *testing.T) {
	h := &fakeDevice{}
	d := newRadio(h)
	results := &macResults{}
	var timer func()
	sm := submac.New(submac.Config{
		Radio: d, Callbacks: results,
		ScheduleTimer: func(_ time.Duration, fn func()) { timer = fn },
		StopTimer:     func() { timer = nil },
	})
	tasks := make(chan func(), 4)
	if err := d.SetCallbacks(sm, func(fn func()) { tasks <- fn }); err != nil {
		t.Fatal(err)
	}
	if err := sm.Enable(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
			return
		}
		select {
		case <-d.Done():
		case <-time.After(time.Second):
			t.Error("dispatcher did not drain")
		}
	})
	frame := sm.GetTransmitFrame()
	frame.PSDU = append(frame.PSDU[:0], 0x21, 0x10, 7)
	frame.TxInfo.TxPower = 9
	frame.TxInfo.IsSecurityProcessed = true
	if err := sm.Send(submac.TxRequest{Channel: 15, CsmaCaEnabled: true, AckRequest: true, MaxCsmaBackoffs: 1, MaxFrameRetries: 1}); err != nil {
		t.Fatal(err)
	}
	step := func(result error) {
		t.Helper()
		if timer == nil {
			t.Fatal("missing software backoff")
		}
		fn := timer
		timer = nil
		fn()
		if !h.lastCCA || h.lastChannel != 15 || h.lastPower != 9 {
			t.Fatal("request/power/security metadata was lost at hardware boundary")
		}
		key := lock()
		d.transmitDone([]byte{2, 0, 7}, radio.RxInfo{}, result)
		unlock(key)
		for i := 0; i < 2; i++ {
			select {
			case dispatch := <-tasks:
				dispatch()
			case <-time.After(time.Second):
				t.Fatal("missing serialized radio callback")
			}
		}
	}
	step(ErrChannelAccessFailure)
	if results.completed != 0 {
		t.Fatal("CCA failure bypassed software retry")
	}
	step(ErrNoAck)
	if results.completed != 0 {
		t.Fatal("no ACK bypassed software frame retry")
	}
	step(nil)
	if results.completed != 1 || results.last != nil || sm.GetTransmitRetries() != 1 || len(results.cca) != 3 || results.cca[0] {
		t.Fatal("incorrect SubMac terminal result", results)
	}
}
