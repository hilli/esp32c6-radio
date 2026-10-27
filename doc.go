// Package esp32c6radio implements the gothread radio.Radio interface for TinyGo
// ESP32-C6 targets. Native PHY libraries are bundled; ESP-IDF is not required.
//
// The target must inherit esp32c6_radio and use esp32c6-radio.ld. The supplied
// target does so. This driver is experimental: no physical RF operation,
// entropy source, durable C6 storage or Thread attachment has been verified.
package esp32c6radio
