package settings

import (
	"bytes"
	"testing"

	"github.com/hilli/gothread/instance"
	"github.com/hilli/gothread/meshcop"
	"github.com/hilli/gothread/platform"
	"github.com/hilli/gothread/thread/key"
)

func sharedInstance(t *testing.T, settings platform.Settings, provision bool) *instance.Instance {
	t.Helper()
	inst := instance.New(instance.Config{Settings: settings})
	if err := inst.PersistenceError(); err != nil {
		t.Fatal(err)
	}
	if provision {
		if err := inst.ActiveDataset().SaveLocalFromInfo(syntheticDataset()); err != nil {
			t.Fatal(err)
		}
	}
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

func TestSharedV2PublicThreadCountersSurviveMatterCompactionAndReboot(t *testing.T) {
	s, f := sharedFixture(t)
	first := sharedInstance(t, s.ThreadSettings(), true)
	mac1, mle1, err := securedCounters(first)
	if err != nil {
		t.Fatal(err)
	}
	f.data[int(s.active)*SharedV2BankSize+int(s.banks[s.active].tail)+8] = 0
	if err := s.MatterBlobs().Set("node/utility_commissioning", bytes.Repeat([]byte{0x71}, 9186)); err != nil {
		t.Fatal(err)
	}
	if s.stats.Compactions != 1 {
		t.Fatal("public Thread state was not copied through Matter compaction")
	}
	ack := s.stats.Generation
	reopened, err := openSharedV2(f.reboot(), owner)
	if err != nil {
		t.Fatal(err)
	}
	second := sharedInstance(t, reopened.ThreadSettings(), false)
	if !bytes.Equal(second.ReadActiveDataset(), meshcop.InfoToDataset(syntheticDataset()).Bytes()) {
		t.Fatal("shared namespace dataset not restored through Instance")
	}
	mac2, mle2, err := securedCounters(second)
	if err != nil {
		t.Fatal(err)
	}
	if mac2 <= mac1 || mle2 <= mle1 || mac2 < key.FrameCounterReservation || mle2 < key.FrameCounterReservation || reopened.stats.Generation < ack {
		t.Fatal("counter reuse after shared-owner reboot", mac1, mle1, mac2, mle2)
	}
	requireSharedBlob(t, reopened, "node/utility_commissioning", bytes.Repeat([]byte{0x71}, 9186))
}

func TestSharedV2PublicThreadReservationFailureClosesBothViews(t *testing.T) {
	s, f := sharedFixture(t)
	inst := sharedInstance(t, s.ThreadSettings(), true)
	if err := s.MatterBlobs().Set("retained", []byte("synthetic")); err != nil {
		t.Fatal(err)
	}
	f.cut, f.mode = f.steps+1, "after"
	if _, _, err := securedCounters(inst); err == nil {
		t.Fatal("failed reservation reached public encryption success")
	}
	if inst.PersistenceError() == nil {
		t.Fatal("stack did not latch reservation failure")
	}
	f.cut = 0
	if _, err := s.MatterBlobs().Get("retained"); err == nil {
		t.Fatal("Matter view ignored whole-owner latch")
	}
	if _, _, err := securedCounters(inst); err == nil {
		t.Fatal("stack resumed after reservation failure")
	}
}
