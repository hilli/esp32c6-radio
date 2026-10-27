//go:build tinygo && esp32c6 && esp32c6_radio

// Controlled storage campaign endpoint. It never writes at boot.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	esp32c6radio "github.com/hilli/esp32c6-radio"
	"github.com/hilli/esp32c6-radio/lab"
	"github.com/hilli/esp32c6-radio/settings"
	"github.com/hilli/gothread/platform"
)

var firmwareID = "UNSET"

const testKey = 0xff00

func main() {
	driver, err := esp32c6radio.New()
	if err != nil {
		println(err.Error())
		select {}
	}
	eui := driver.GetIEEEEui64()
	identity := hex.EncodeToString(eui[:])
	var store *settings.Store
	session := lab.Session{FirmwareID: firmwareID, DeviceEUI: identity}
	input := bufio.NewScanner(lab.SerialReader{})
	input.Buffer(make([]byte, 1024), 1024)
	for input.Scan() {
		c, err := lab.ParseCommand(input.Bytes())
		if err != nil {
			println("invalid laboratory command")
			continue
		}
		if session.Arm(c) != nil || c.DelayMillis > 2000 {
			println("laboratory identity/build scope is not armed")
			continue
		}
		emit := func(stage string, status lab.Status, reason string, counter uint64) {
			e := lab.Event{Probe: "storage", Stage: stage, Status: status, Counter: counter, Reason: reason}
			if store != nil {
				e.Generation = store.Statistics().Generation
			}
			p, err := session.Record(e)
			if err != nil {
				println("evidence encoding failed")
				return
			}
			fmt.Fprint(os.Stdout, string(p))
		}
		if c.Operation == "hello" {
			emit("hello", lab.Pass, "identity only; no flash mutation", 0)
			continue
		}
		if c.Operation == "format" {
			if c.Confirmation != identity+":0x3f8000:"+c.RunID {
				emit("format", lab.Fail, "explicit disposable partition confirmation missing", 0)
				continue
			}
			if store != nil {
				emit("format", lab.Fail, "close/reboot before formatting", 0)
				continue
			}
			err = settings.FormatEmpty(driver)
			if err != nil {
				emit("format", lab.Fail, err.Error(), 0)
			} else {
				emit("format", lab.Pass, "erased partition initialized; physical durability not certified", 0)
			}
			continue
		}
		if store == nil {
			store, err = settings.Open(driver)
			if err != nil {
				emit("open", lab.Fail, err.Error(), 0)
				continue
			}
		}
		switch c.Operation {
		case "observe":
			p, err := store.Get(testKey, 0)
			if err != nil {
				emit("observed", lab.Fail, err.Error(), 0)
				continue
			}
			if len(p) != 8 {
				emit("observed", lab.Fail, "invalid test counter length", 0)
				continue
			}
			emit("observed", lab.Pass, "reconcile against independent host acknowledgement ledger", binary.LittleEndian.Uint64(p))
		case "write":
			if c.Confirmation != identity+":0x3f8000:"+c.RunID {
				emit("prepare_write", lab.Fail, "write scope missing", 0)
				continue
			}
			previous, getErr := store.Get(testKey, 0)
			if getErr != nil && !errors.Is(getErr, platform.ErrSettingsNotFound) {
				emit("write", lab.Fail, getErr.Error(), 0)
				continue
			}
			if getErr == nil && (len(previous) != 8 || c.Counter <= binary.LittleEndian.Uint64(previous)) {
				emit("write", lab.Fail, "test counter must strictly increase", 0)
				continue
			}
			if err := store.SetObserver(func(step settings.Step) {
				e := lab.Event{Probe: "storage", Stage: step.Phase, Status: lab.NotRun, Generation: step.Generation, Metrics: map[string]uint64{"offset": uint64(step.Offset), "bytes": uint64(step.Bytes)}, Reason: "controlled cut checkpoint, not an acknowledgement"}
				p, err := session.Record(e)
				if err != nil {
					println("checkpoint evidence encoding failed")
					return
				}
				fmt.Fprint(os.Stdout, string(p))
				if c.DelayMillis > 0 && len(step.Phase) > 7 && step.Phase[:7] == "before_" {
					time.Sleep(time.Duration(c.DelayMillis) * time.Millisecond)
				}
			}); err != nil {
				emit("write", lab.Fail, err.Error(), 0)
				continue
			}
			emit("prepare_write", lab.NotRun, "manual controlled-cut window; no acknowledgement yet", c.Counter)
			p := binary.LittleEndian.AppendUint64(nil, c.Counter)
			if err := store.Set(testKey, p); err != nil {
				emit("write", lab.Fail, err.Error(), 0)
				continue
			}
			emit("ack", lab.Pass, "native operation completed and committed bytes read back", c.Counter)
		default:
			emit("command", lab.Fail, "unsupported operation; no flash mutation", 0)
		}
	}
	if err := input.Err(); err != nil {
		println("laboratory input failed:", err.Error())
	}
	select {}
}
