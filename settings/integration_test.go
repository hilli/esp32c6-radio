package settings

import (
	"bytes"
	"errors"
	"testing"

	"github.com/hilli/gothread/instance"
	"github.com/hilli/gothread/meshcop"
	"github.com/hilli/gothread/radio"
	"github.com/hilli/gothread/thread/key"
	"github.com/hilli/gothread/thread/mesh"
	"github.com/hilli/gothread/thread/mle"
)

// Entirely synthetic, host-only dataset. It is never a device credential or
// evidence of physical C6 flash/entropy/RF operation.
func syntheticDataset() *meshcop.Info {
	p := &meshcop.Info{NetworkKey: [16]byte{0x12, 0x34, 0x56}, Channel: 20, PanID: 0xabcd, NetworkName: "synthetic",
		Components: meshcop.Components{NetworkKey: true, Channel: true, PanID: true, NetworkName: true, ActiveTimestamp: true}}
	p.ActiveTimestamp.SetSeconds(42)
	return p
}
func setupInstance(t *testing.T, store *Store, provision bool) *instance.Instance {
	t.Helper()
	inst := instance.New(instance.Config{Settings: store})
	if err := inst.PersistenceError(); err != nil {
		t.Fatal(err)
	}
	if provision {
		if err := inst.ActiveDataset().SaveLocalFromInfo(syntheticDataset()); err != nil {
			t.Fatal(err)
		}
	}
	// No goroutine or fake radio: these are the actual public stack paths before
	// Start, permitted by Instance's single-loop construction contract.
	if err := inst.IfConfigUp(); err != nil {
		t.Fatal(err)
	}
	if err := inst.ThreadStart(); err != nil {
		t.Fatal(err)
	}
	if err := inst.ThreadStop(); err != nil {
		t.Fatal(err)
	}
	return inst
}
func securedCounters(inst *instance.Instance) (uint32, uint32, error) {
	msg := mle.NewMessage(mle.CommandAdvertisement)
	if err := inst.MleService().Dispatcher().SendMessage(msg, mle.LinkLocalAllNodesMulticast); err != nil {
		return 0, 0, err
	}
	f := &radio.Frame{}
	if !inst.HandleFrameRequest(f) {
		return 0, 0, errors.Join(errors.New("public frame builder did not emit"), inst.PersistenceError())
	}
	ctx, _, err := mesh.NewFramer().ParseFrameHeader(f.PSDU)
	if err != nil {
		return 0, 0, err
	}
	inst.HandleSentFrame(f, nil)
	pm, err := mle.ParseMessage(msg.Bytes())
	if err != nil {
		return 0, 0, err
	}
	return ctx.Security.FrameCounter, pm.SecHeader.FrameCounter, nil
}
func TestPublicGothreadRebootUsesAcknowledgedJournalReservations(t *testing.T) {
	store, flash := formatted(t)
	first := setupInstance(t, store, true)
	mac1, mle1, err := securedCounters(first)
	if err != nil {
		t.Fatal(err)
	}
	ack := store.Statistics().Generation
	reopened, err := open(flash.reboot(), owner)
	if err != nil {
		t.Fatal(err)
	}
	second := setupInstance(t, reopened, false)
	if !bytes.Equal(second.ReadActiveDataset(), meshcop.InfoToDataset(syntheticDataset()).Bytes()) {
		t.Fatal("dataset not restored through public Instance")
	}
	mac2, mle2, err := securedCounters(second)
	if err != nil {
		t.Fatal(err)
	}
	if mac2 <= mac1 || mle2 <= mle1 || mac2 < key.FrameCounterReservation || mle2 < key.FrameCounterReservation || reopened.Statistics().Generation < ack {
		t.Fatal("public encrypted path reused a reserved counter", mac1, mle1, mac2, mle2)
	}
}
func TestPublicGothreadStopsOnAmbiguousReservation(t *testing.T) {
	store, flash := formatted(t)
	inst := setupInstance(t, store, true)
	flash.cut, flash.mode = flash.steps+1, "after"
	if _, _, err := securedCounters(inst); err == nil {
		t.Fatal("ambiguous counter write reached encryption success")
	}
	if inst.PersistenceError() == nil {
		t.Fatal("public stack did not latch storage error")
	}
	flash.cut = 0
	if _, _, err := securedCounters(inst); err == nil {
		t.Fatal("public stack resumed after ambiguous write")
	}
}
