// Package threadnode demonstrates always-on end-device wiring. It deliberately
// supplies no RAM Settings fallback: an application must provide storage whose
// atomicity and durability have been verified on its actual ESP32-C6 board.
package threadnode

import (
	"errors"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/gothread/instance"
	"github.com/hilli/gothread/mac/types"
	"github.com/hilli/gothread/meshcop"
	"github.com/hilli/gothread/platform"
	"github.com/hilli/gothread/thread/mle"
)

var ErrDurableSettingsRequired = errors.New("thread-node: verified durable Settings required; TinyGo 0.42 has no C6 Flash driver")
var ErrIncompleteDataset = errors.New("thread-node: a complete preconfigured active dataset is required")
var ErrIdentityMismatch = errors.New("thread-node: Instance and radio factory identities disagree")

// Run provisions or restores a lab dataset and starts an always-on minimal end
// device, never a router or sleepy child. Pass a complete dataset for initial
// provisioning, or nil to restore. Closing stop requests an orderly shutdown.
//
// Preconditions: physical PHY entropy validation and an exclusively owned,
// atomic, durable, non-rollback Settings namespace. A non-nil interface alone
// cannot prove these guarantees. See docs/storage.md before using credentials.
// This wiring example is not a standalone flashable firmware.
func Run(settings platform.Settings, dataset *meshcop.Info, stop <-chan struct{}) (result error) {
	if settings == nil {
		return ErrDurableSettingsRequired
	}
	driver, err := esp32c6radio.New()
	if err != nil {
		return err
	}
	var inst *instance.Instance
	running := false
	defer func() {
		if err := driver.Close(); err != nil {
			// A failed hardware quiescence keeps its buffers and running loop
			// alive until reset, rather than allowing a late blocked PostTask.
			result = errors.Join(result, err)
			return
		}
		<-driver.Done()
		if running {
			inst.Stop()
		}
	}()
	if err := driver.Enable(); err != nil {
		return err
	}
	inst = instance.New(instance.Config{Radio: driver, Entropy: driver, Settings: settings})
	if err := inst.PersistenceError(); err != nil {
		return err
	}
	if inst.Mac().GetExtAddress() != types.ExtAddress(driver.GetIEEEEui64()) {
		return ErrIdentityMismatch
	}
	if dataset != nil {
		if err := inst.ActiveDataset().SaveLocalFromInfo(dataset); err != nil {
			return err
		}
	}
	stored, err := inst.ActiveDataset().Read()
	if err != nil {
		return err
	}
	if !stored.ContainsAllRequiredTLVsFor(meshcop.DatasetActive) {
		return ErrIncompleteDataset
	}
	var mode mle.DeviceMode
	mode.Set(mle.ModeRxOnWhenIdle)
	inst.MleService().Mle().SetDeviceMode(mode)
	inst.MleService().Mle().SetRouterEligible(false)
	inst.Mac().SetRxOnWhenIdle(true)
	if err := driver.SetCallbacks(inst.SubMac(), inst.PostTask); err != nil {
		return err
	}
	if err := inst.Start(); err != nil {
		return err
	}
	running = true
	started := make(chan error, 1)
	inst.PostTask(func() {
		if err := inst.IfConfigUp(); err != nil {
			started <- err
			return
		}
		started <- inst.ThreadStart()
	})
	if err := <-started; err != nil {
		return err
	}
	<-stop
	stopped := make(chan error, 1)
	inst.PostTask(func() {
		if inst.IsThreadRunning() {
			stopped <- inst.ThreadStop()
		} else {
			stopped <- nil
		}
	})
	return errors.Join(<-stopped, inst.PersistenceError())
}
