//go:build tinygo && esp32c6

// Package chip contains the ESP32-C6-only modem bring-up path.
package chip

import (
	"device/esp"
	"device/riscv"
	"errors"
	"runtime/interrupt"
	"runtime/volatile"
	"time"
	"unsafe"

	"github.com/hilli/esp32c6-radio/internal/phy"
)

// Register layout and commands: ESP-IDF v5.5 ieee802154_struct.h and
// ieee802154_common_ll.h, not the unrelated RF frontend at 0x600a0000.
const (
	macBase       = uintptr(0x600a3000)
	regCmd        = 0x00
	regConf       = 0x04
	regEventEn    = 0x60
	regEvents     = 0x64
	regTimer0     = 0xa8
	cmdStop       = 0x45
	cmdTimerStart = 0x4c
	cmdTimerStop  = 0x4d
	cpuIRQ        = 11 // TinyGo reserves 6 (GPIO), 7 (UART), 9 (timer), 10 (USB).
	timerEvent    = 1 << 8
)

var (
	irqCount volatile.Register32
	ready    bool
	probing  bool
	handler  func(uint32)
)

func reg(offset uintptr) *volatile.Register32 {
	return (*volatile.Register32)(unsafe.Pointer(macBase + offset))
}

// Init exclusively takes the modem for IEEE 802.15.4. Wi-Fi, BLE and low-power
// PMU transitions must not be used concurrently.
func Init() (int, error) {
	if ready {
		return 0, errors.New("esp32c6-radio: modem already initialized")
	}
	enableModem()
	result := phy.Calibrate()
	if result != 0 && result != 1 {
		phy.Close()
		return result, errors.New("esp32c6-radio: reserved PHY calibration result")
	}
	phy.InitBaseband()
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_ZBMAC(1)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_ZBMAC(0)
	reg(regEventEn).Set(0)
	reg(regCmd).Set(cmdStop)
	reg(regEvents).Set(0x1fff) // write-one-to-clear, no read/modify/write
	// No DMA or RF operation is started by this feasibility probe.
	// Configure legacy hardware ACKs and PAN 0; enhanced ACKs stay disabled.
	const configMask = 1 | 1<<1 | 1<<3 | 1<<6 | 1<<7 | 1<<12 | 0xf<<28
	reg(regConf).Set(reg(regConf).Get()&^configMask | 1 | 1<<3 | 1<<28)
	reg(0x48).Set(3)      // channel 11
	reg(0x4c).Set(8)      // 0 dBm
	reg(0x54).Set(0x60b5) // ED CCA, averaged samples, -75 dBm
	reg(0x110).Set(50)    // RF receive-on delay, microseconds
	reg(0x128).Set(0)     // MAC security remains in gothread
	if err := configureIRQ(); err != nil {
		phy.Close()
		return result, err
	}
	ready = true
	return result, nil
}

func configureIRQ() error {
	mask := interrupt.Disable()
	defer interrupt.Restore(mask)
	esp.INTERRUPT_CORE0.ZB_MAC_INTR_MAP.Set(cpuIRQ)
	irq := interrupt.New(cpuIRQ, handleIRQ)
	if err := irq.Enable(); err != nil {
		return err
	}
	// Override TinyGo's default edge mode: the MAC raises a level until its
	// W1C event status has been cleared. Priority must exceed threshold 5.
	plic := (*[37]volatile.Register32)(unsafe.Pointer(uintptr(0x20001000)))
	plic[1].ClearBits(1 << cpuIRQ)
	plic[4+cpuIRQ].Set(6)
	riscv.Asm("fence")
	return nil
}

func handleIRQ(interrupt.Interrupt) {
	events := reg(regEvents).Get()
	reg(regEvents).Set(events)
	if probing && events&timerEvent != 0 {
		reg(regCmd).Set(cmdTimerStop)
		reg(regEventEn).ClearBits(timerEvent)
		irqCount.Set(irqCount.Get() + 1)
	} else if handler != nil {
		handler(events)
	}
}

// ProbeIRQ exercises the real MAC timer -> interrupt matrix -> PLIC -> Go ISR
// path. It must run on a board; linking it is not evidence that it passed.
func ProbeIRQ() error {
	if !ready {
		return errors.New("esp32c6-radio: modem is not initialized")
	}
	before := irqCount.Get()
	probing = true
	reg(regEvents).Set(timerEvent)
	reg(regTimer0).Set(1000)
	reg(regEventEn).Set(timerEvent)
	reg(regCmd).Set(cmdTimerStart)
	deadline := time.Now().Add(20 * time.Millisecond)
	for irqCount.Get() == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	reg(regCmd).Set(cmdTimerStop)
	reg(regEventEn).Set(0)
	probing = false
	if irqCount.Get() == before {
		return errors.New("esp32c6-radio: MAC timer IRQ probe timed out")
	}
	return nil
}

func SetHandler(fn func(uint32)) { handler = fn }

func Shutdown() error {
	key := interrupt.Disable()
	reg(regEventEn).Set(0)
	reg(regCmd).Set(cmdStop)
	reg(regEvents).Set(0x1fff)
	esp.INTERRUPT_CORE0.ZB_MAC_INTR_MAP.Set(0)
	handler = nil
	interrupt.Restore(key)
	deadline := time.Now().Add(20 * time.Millisecond)
	for reg(0x88).Get()&0xf00 != 0 {
		if time.Now().After(deadline) {
			return errors.New("esp32c6-radio: MAC did not quiesce; reset required")
		}
		time.Sleep(100 * time.Microsecond)
	}
	phy.Close()
	ready = false
	return nil
}

func enableModem() {
	// Keep the shared modem powered and out of isolation in active-only mode.
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_PD(0)
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_RESET(0)
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_ISO(0)
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_PU(1)
	time.Sleep(10 * time.Microsecond)
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_NO_ISO(1)
	esp.PMU.SetPOWER_PD_HPWIFI_CNTL_FORCE_HP_WIFI_NO_RESET(1)
	esp.PMU.SetRF_PWC_PERIF_I2C_RSTB(1)
	esp.PMU.SetRF_PWC_XPD_PERIF_I2C(1)
	esp.PMU.SetHP_ACTIVE_ICG_MODEM_HP_ACTIVE_DIG_ICG_MODEM_CODE(2)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_MODEM_APB_ST_MAP(4)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_MODEM_PERI_ST_MAP(4)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_WIFI_ST_MAP(4)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_BT_ST_MAP(4)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_FE_ST_MAP(4)
	esp.MODEM_SYSCON.SetCLK_CONF_POWER_ST_CLK_ZB_ST_MAP(4)
	esp.MODEM_LPCON.SetCLK_CONF_POWER_ST_CLK_LP_APB_ST_MAP(4)
	esp.MODEM_LPCON.SetCLK_CONF_POWER_ST_CLK_I2C_MST_ST_MAP(4)
	esp.MODEM_LPCON.SetCLK_CONF_POWER_ST_CLK_COEX_ST_MAP(4)
	esp.PMU.SetIMM_MODEM_ICG_UPDATE_DIG_ICG_MODEM_EN(1)
	esp.PMU.SetIMM_SLEEP_SYSCLK_UPDATE_DIG_ICG_SWITCH(1)

	esp.MODEM_LPCON.SetCLK_CONF_CLK_I2C_MST_EN(1)
	esp.MODEM_LPCON.SetI2C_MST_CLK_CONF_CLK_I2C_MST_SEL_160M(1)
	esp.MODEM_LPCON.SetCLK_CONF_CLK_COEX_EN(1)
	// Full calibration also accesses Wi-Fi BB and BT clocks, even though
	// neither Wi-Fi nor BLE is enabled.
	esp.MODEM_SYSCON.CLK_CONF1.SetBits(0x1ff | 1<<17 | 1<<18)
	esp.MODEM_SYSCON.SetCLK_CONF1_CLK_FE_APB_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF1_CLK_FE_CAL_160M_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF1_CLK_FE_160M_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF1_CLK_FE_80M_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_ETM_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_ZB_APB_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_ZB_MAC_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_MODEM_SEC_APB_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_MODEM_SEC_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_MODEM_SEC_ECB_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_MODEM_SEC_CCM_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_MODEM_SEC_BAH_EN(1)
	esp.MODEM_SYSCON.SetCLK_CONF_CLK_BLE_TIMER_EN(1)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_FE(1)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_BTBB(1)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_BTBB_APB(1)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_FE(0)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_BTBB(0)
	esp.MODEM_SYSCON.SetMODEM_RST_CONF_RST_BTBB_APB(0)
}
